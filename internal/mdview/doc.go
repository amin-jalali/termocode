// Package mdview renders a Markdown document as a terminal "reader view":
// a centred reading column with clear heading levels, hanging list
// indents, framed code blocks with syntax colours, box-drawn tables and
// clickable (OSC 8) links. It draws with the active theme tokens and icon
// mode, and every returned row is exactly the requested width.
//
// The full-screen Markdown preview (internal/app/preview.go) and the LSP
// hover view use it; the AI chat panel has its own streaming renderer.
package mdview
