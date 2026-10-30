// Package assets holds the browser UI of the web gateway: TypeScript
// sources in src/, vendored libraries in vendor/ (pinned by sha384 in
// vendor/vendor.json) and the built files cmd/webbuild writes here.
// The built files are committed; CI rebuilds them and fails on a diff.
//
// Type checking: there is no node and no tsc anywhere (owner rule), and
// esbuild strips types without checking them. types_test.go checks what
// can be checked by reading the sources: every named import (value or
// type) names a real export, vendored imports name real exports of the
// vendored JS, and every field the schema guard reads exists on the
// webproto Go type. Expression types, call arguments, JSX props and null
// checks are NOT checked; the browser tests in e2e/web (build tag
// browser) are the safety net for what they exercise.
package assets

//go:generate go run ../../../cmd/webbuild
