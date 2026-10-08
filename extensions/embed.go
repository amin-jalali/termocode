// Package extensions embeds the sample extensions that ship with
// termocode. Each folder is a normal extension (init.lua +
// extension.json); "Extensions: Install Sample Extensions" copies them
// into ~/.config/termocode/extensions/. See docs/extensions.md.
package extensions

import "embed"

// FS holds one folder per sample extension.
//
//go:embed todo-tree word-count open-in-github rest-client
var FS embed.FS
