set shell := ["bash", "-euo", "pipefail", "-c"]

pkg     := "chronicle"
version := `sed -n 's/^const Number = "\(.*\)"/\1/p' internal/version/version.go`
commit  := `git rev-parse --short HEAD 2>/dev/null || true`
dirty   := `test -z "$(git status --porcelain 2>/dev/null)" || echo ".dirty"`
ldflags := "-X " + pkg + "/internal/version.Commit=" + commit + dirty

default:
    @just --list

# Run the relay from source
dev:
    go run .

# Build ./chronicle with the commit hash injected
build:
    go build -ldflags "{{ldflags}}" -o chronicle .

# Vet and run the tests
test:
    go vet ./...
    go test ./...

# Print the version constant and the latest tag
version:
    @echo "code: {{version}}"
    @echo "tag:  $(git describe --tags --abbrev=0 2>/dev/null || echo none)"

# Cross-compile chronicle and migrate into dist/ as <name>-<version>-<os>-<arch>
dist:
    #!/usr/bin/env bash
    set -euo pipefail
    rm -rf dist && mkdir dist
    for target in linux/amd64 linux/arm64 darwin/arm64; do
        os="${target%/*}"; arch="${target#*/}"
        for name in chronicle migrate; do
            src="."; [[ "$name" == chronicle ]] || src="./cmd/$name"
            out="dist/$name-{{version}}-$os-$arch"
            CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "-s -w {{ldflags}}" -o "$out" "$src"
            echo "$out"
        done
    done

# Build for linux and swap the binary on the server: just deploy user@host
deploy target:
    GOOS=linux GOARCH=amd64 go build -ldflags "{{ldflags}}" -o chronicle-linux-amd64 .
    rsync --progress chronicle-linux-amd64 {{target}}:chronicle/chronicle-new
    ssh {{target}} 'systemctl stop chronicle'
    ssh {{target}} 'mv chronicle/chronicle-new chronicle/chronicle'
    ssh {{target}} 'systemctl start chronicle'

# Same as deploy, with the LMDB backend compiled in Docker (needs cgo)
deploy_lmdb target:
    docker build -f compileDockerfile --platform linux/amd64 --build-arg LDFLAGS="{{ldflags}}" -t chronicle-app .
    docker create --name temp-container --platform linux/amd64 chronicle-app
    docker cp temp-container:/app/chronicle-linux-amd64 ./chronicle-linux-amd64
    docker rm temp-container
    rsync --progress chronicle-linux-amd64 {{target}}:chronicle/chronicle-new
    ssh {{target}} 'systemctl stop chronicle'
    ssh {{target}} 'mv chronicle/chronicle-new chronicle/chronicle'
    ssh {{target}} 'systemctl start chronicle'

# Add an entry to the changelog: just log "Fix something"
log entry:
    #!/usr/bin/env bash
    set -euo pipefail
    grep -q '^## \[Unreleased\]' CHANGELOG.md || { echo "no Unreleased section in CHANGELOG.md"; exit 1; }
    ENTRY={{quote(entry)}} awk '
        /^## \[Unreleased\]/ {print; print ""; print "- " ENVIRON["ENTRY"]; ins=1; next}
        ins && /^$/ {next}
        ins && /^## / {print ""; print; ins=0; next}
        ins {print; ins=0; next}
        {print}
    ' CHANGELOG.md > CHANGELOG.md.tmp && mv CHANGELOG.md.tmp CHANGELOG.md
    echo "Unreleased is now:"
    awk '/^## \[Unreleased\]/{f=1;next} /^## \[/{f=0} f' CHANGELOG.md

# Bump the version, update the changelog, commit and tag (no push)
release VERSION:
    #!/usr/bin/env bash
    set -euo pipefail
    v="{{VERSION}}"
    [[ "$v" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "version must be X.Y.Z"; exit 1; }
    [[ -z "$(git status --porcelain)" ]] || { echo "working tree not clean"; exit 1; }
    ! git rev-parse -q --verify "refs/tags/v$v" >/dev/null || { echo "tag v$v exists"; exit 1; }
    grep -q '^## \[Unreleased\]' CHANGELOG.md || { echo "no Unreleased section in CHANGELOG.md"; exit 1; }
    [[ -n "$(awk '/^## \[Unreleased\]/{f=1;next} /^## \[/{f=0} f && /^- /' CHANGELOG.md)" ]] || { echo "no entries under Unreleased in CHANGELOG.md"; exit 1; }
    just test
    sed -i '' "s/^const Number = \".*\"/const Number = \"$v\"/" internal/version/version.go
    sed -i '' "s/^## \[Unreleased\]/## [Unreleased]\n\n## [$v] - $(date +%Y-%m-%d)/" CHANGELOG.md
    git add internal/version/version.go CHANGELOG.md
    git commit -m "Release v$v"
    git tag -a "v$v" -m "v$v"
    echo "Released v$v, push with: git push --follow-tags"

# Create the GitHub release for the current version with dist/ attached; `just publish draft` for a draft
publish MODE="":
    #!/usr/bin/env bash
    set -euo pipefail
    v="{{version}}"; tag="v$v"
    [[ -z "$(git status --porcelain)" ]] || { echo "working tree not clean"; exit 1; }
    git rev-parse -q --verify "refs/tags/$tag" >/dev/null || { echo "tag $tag not found, run: just release $v"; exit 1; }
    [[ -n "$(git ls-remote --tags origin "$tag")" ]] || { echo "tag $tag not on origin, run: git push --follow-tags"; exit 1; }
    ! gh release view "$tag" >/dev/null 2>&1 || { echo "release $tag already exists"; exit 1; }
    notes="$(awk -v h="## [$v]" 'index($0,h)==1{f=1;next} /^## \[/{f=0} f' CHANGELOG.md)"
    draft=""
    [[ "{{MODE}}" != "draft" ]] || draft="--draft"
    just dist
    gh release create "$tag" dist/* --title "$tag" --notes "$notes" $draft

# Release, push and publish in one go; `just ship X.Y.Z draft` for a draft release
ship VERSION MODE="":
    just release {{VERSION}}
    git push --follow-tags
    just publish {{MODE}}

# Undo the last release: `just undo release` (local only) | `just undo ship` (also deletes the github release and remote tag)
undo action:
    #!/usr/bin/env bash
    set -euo pipefail
    v="{{version}}"; tag="v$v"
    [[ "{{action}}" == "release" || "{{action}}" == "ship" ]] || { echo "unknown action: {{action}} (release | ship)"; exit 1; }
    [[ "$(git log -1 --format=%s)" == "Release $tag" ]] || { echo "last commit is not 'Release $tag', nothing to undo"; exit 1; }
    [[ -z "$(git status --porcelain)" ]] || { echo "working tree not clean"; exit 1; }
    on_origin="$(git ls-remote --tags origin "$tag")"
    if [[ "{{action}}" == "release" ]]; then
        [[ -z "$on_origin" ]] || { echo "tag $tag is already on origin, use: just undo ship"; exit 1; }
        read -r -p "Undo local release $tag (drop the tag and reset the last commit)? [y/N] " a
        [[ "$a" == "y" || "$a" == "Y" ]] || exit 1
    else
        read -r -p "Undo shipped $tag (delete the github release and remote tag, force-push the removal)? [y/N] " a
        [[ "$a" == "y" || "$a" == "Y" ]] || exit 1
        ! gh release view "$tag" > /dev/null 2>&1 || gh release delete "$tag" --yes
        [[ -z "$on_origin" ]] || git push origin ":refs/tags/$tag"
    fi
    ! git rev-parse -q --verify "refs/tags/$tag" > /dev/null || git tag -d "$tag"
    git reset --hard HEAD~1
    if [[ "{{action}}" == "ship" ]]; then
        git push --force-with-lease origin main
        echo "Shipped $tag undone; note that anyone who already fetched the tag keeps it"
    else
        echo "Release $tag undone"
    fi
