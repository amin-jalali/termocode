// Package diagram draws small diagrams written as text in fenced Markdown
// blocks: Mermaid flowcharts and sequence diagrams, CSV / TSV tables and
// Excalidraw scenes. The result is plain text rows (box-drawing
// characters, no colour) no wider than the asked width, so the Markdown
// preview, the AI chat and the web page can show them the same way.
// When a block does not parse, uses a kind not drawn yet, or does not fit,
// Render reports false and the caller shows the source.
package diagram
