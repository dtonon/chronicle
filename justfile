deploy target:
    docker build --platform linux/amd64 -t wot-relay-app .
    docker create --name temp-container wot-relay-app
    docker cp temp-container:/app/main ./wot-relay
    docker rm temp-container
    # GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=$(git describe --tags --always)"
    rsync --progress wot-relay {{target}}:wot-relay/wot-relay-new
    ssh {{target}} 'systemctl stop wot-relay'
    ssh {{target}} 'mv wot-relay/wot-relay-new wot-relay/wot-relay'
    ssh {{target}} 'systemctl start wot-relay'