// Package assets holds the browser UI of the web gateway: TypeScript
// sources in src/, vendored libraries in vendor/ (pinned by sha384 in
// vendor/vendor.json) and the built files cmd/webbuild writes here.
// The built files are committed; CI rebuilds them and fails on a diff.
//
// Type checking: CI has no node and no tsc (owner rule), and esbuild
// strips types without checking them. types_test.go reads the sources and
// checks named imports and schema fields; it runs everywhere. On a dev
// machine with tsc, run the full strict check (tsconfig.json maps preact,
// preact/hooks and @preact/signals to the vendored .d.ts files) with:
//
//	TERMOCODE_TSC=1 go test -run TestTypeScriptTypeChecks ./internal/webgw/assets
//
// or directly: tsc -p internal/webgw/assets/tsconfig.json. Keep it at zero
// errors before you commit TypeScript changes.
package assets

//go:generate go run ../../../cmd/webbuild
