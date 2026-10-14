package main

import "runtime/debug"

// version is set at release build time:
//
//	go build -ldflags "-X main.version=v1.2.3" ./cmd/termocode
//
// Empty means a local or `go install` build; see versionString.
var version = ""

// versionString is what `termocode --version` prints: the release
// version, else the module version `go install …@vX` recorded, else
// "dev".
func versionString() string {
	return pickVersion(version, debug.ReadBuildInfo)
}

func pickVersion(linked string, info func() (*debug.BuildInfo, bool)) string {
	if linked != "" {
		return linked
	}
	if bi, ok := info(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// isVersionArg reports whether arg asks for the version.
func isVersionArg(arg string) bool {
	return arg == "--version" || arg == "-v" || arg == "version"
}
