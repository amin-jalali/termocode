// Package assets holds the browser UI of the web gateway: TypeScript
// sources in src/, vendored libraries in vendor/ (pinned by sha384 in
// vendor/vendor.json) and the built files cmd/webbuild writes here.
// The built files are committed; CI rebuilds them and fails on a diff.
package assets

//go:generate go run ../../../cmd/webbuild
