package main

import "runtime/debug"

// init fills in the version when the build did not. `make build` stamps it
// with -ldflags, which `go install github.com/mobiumdev/mobium/cmd/mobium@...`
// cannot, so a binary installed that way used to report "dev" — the one
// install route that needs no clone, reporting nothing a bug report could
// use. The toolchain records the module version in every binary it builds:
// the tag once one exists, and the exact commit before then.
func init() {
	if version != "dev" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			version = v
		}
	}
}
