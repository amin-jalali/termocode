package app

import (
	"path/filepath"
	"strings"
)

// supportedExtensions is the set of file extensions termocode opens in the
// editor. Anything outside this set (binaries, images, archives, etc.) is
// refused at the open-call sites — opening a binary in nvim either renders
// as garbage or hangs, which is worse than a polite "not supported" toast.
//
// Keys are lowercase, with the leading dot. Membership lookups use the
// lowercased extension via filepath.Ext.
var supportedExtensions = map[string]struct{}{
	// programming languages
	".go": {}, ".py": {}, ".pyi": {}, ".js": {}, ".jsx": {}, ".mjs": {}, ".cjs": {},
	".ts": {}, ".tsx": {}, ".rs": {}, ".java": {}, ".kt": {}, ".kts": {},
	".c": {}, ".h": {}, ".cc": {}, ".cpp": {}, ".cxx": {}, ".hpp": {}, ".hxx": {},
	".cs": {}, ".rb": {}, ".php": {}, ".swift": {}, ".scala": {}, ".clj": {},
	".cljs": {}, ".cljc": {}, ".ex": {}, ".exs": {}, ".erl": {}, ".elm": {},
	".hs": {}, ".ml": {}, ".mli": {}, ".lua": {}, ".pl": {}, ".pm": {}, ".r": {},
	".dart": {}, ".vue": {}, ".svelte": {}, ".gd": {}, ".nim": {}, ".zig": {},
	".v": {}, ".d": {}, ".pas": {}, ".f": {}, ".f90": {}, ".jl": {}, ".sol": {},
	".m": {}, ".mm": {}, ".scm": {}, ".lisp": {}, ".asm": {}, ".s": {},

	// docs / prose
	".md": {}, ".markdown": {}, ".txt": {}, ".rst": {}, ".org": {}, ".tex": {},
	".adoc": {}, ".asciidoc": {}, ".log": {},

	// markup / web
	".html": {}, ".htm": {}, ".xml": {}, ".xhtml": {}, ".svg": {},
	".css": {}, ".scss": {}, ".sass": {}, ".less": {}, ".styl": {},

	// data / config
	".json": {}, ".jsonc": {}, ".json5": {}, ".yaml": {}, ".yml": {},
	".toml": {}, ".ini": {}, ".cfg": {}, ".conf": {}, ".config": {}, ".env": {},
	".properties": {}, ".csv": {}, ".tsv": {}, ".plist": {},

	// shell / scripts
	".sh": {}, ".bash": {}, ".zsh": {}, ".fish": {}, ".ps1": {}, ".bat": {}, ".cmd": {},

	// query / schema
	".sql": {}, ".graphql": {}, ".gql": {}, ".proto": {}, ".thrift": {},

	// build / infra
	".dockerfile": {}, ".cmake": {}, ".gradle": {}, ".pom": {}, ".bazel": {},
	".bzl": {}, ".tf": {}, ".tfvars": {}, ".hcl": {}, ".nomad": {},

	// editor / vim
	".vim": {}, ".nvim": {},

	// misc text
	".diff": {}, ".patch": {}, ".gitignore": {}, ".dockerignore": {},
	".editorconfig": {}, ".gitattributes": {},
}

// supportedBasenames are filenames (without extension) that termocode opens
// even though they have no dot suffix. Match is case-insensitive against the
// full basename — e.g. "Makefile", "makefile", "MAKEFILE" all qualify.
var supportedBasenames = map[string]struct{}{
	"makefile":     {},
	"dockerfile":   {},
	"readme":       {},
	"license":      {},
	"licence":      {},
	"changelog":    {},
	"changes":      {},
	"authors":      {},
	"contributors": {},
	"contributing": {},
	"todo":         {},
	"notice":       {},
	"copying":      {},
	"version":      {},
	"jenkinsfile":  {},
	"vagrantfile":  {},
	"procfile":     {},
	"caddyfile":    {},
	"justfile":     {},
	"taskfile":     {},
}

// isSupportedFile reports whether termocode should open the file at path
// in the editor. The check is purely name-based — extension first, then a
// known-basenames whitelist for files like Makefile that lack a suffix.
//
// Files with unknown / no extension AND not on the basename list are
// treated as binary / unsupported, and the caller should toast instead
// of opening them.
func isSupportedFile(path string) bool {
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(base))
	if ext != "" {
		_, ok := supportedExtensions[ext]
		return ok
	}
	_, ok := supportedBasenames[strings.ToLower(base)]
	return ok
}

// unsupportedFileMessage returns the toast title + detail shown when a
// user tries to open an unsupported file. Title is the short header; detail
// names the file or extension that was rejected.
func unsupportedFileMessage(path string) (title, detail string) {
	ext := strings.ToLower(filepath.Ext(path))
	title = "Unsupported file"
	if ext == "" {
		detail = filepath.Base(path)
	} else {
		detail = ext + " files are not supported"
	}
	return title, detail
}
