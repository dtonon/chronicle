// Package version holds the release number, bumped by `just release`.
package version

import "runtime/debug"

const Number = "0.6.1"

// Commit is set at build time with -ldflags "-X .../version.Commit=<hash>".
var Commit string

// String returns the number, with the commit suffix when known.
func String() string {
	c := Commit
	if c == "" {
		c = vcsCommit()
	}
	if c == "" {
		return Number
	}
	return Number + "+" + c
}

func vcsCommit() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev, dirty string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = ".dirty"
			}
		}
	}
	if rev == "" {
		return ""
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	return rev + dirty
}
