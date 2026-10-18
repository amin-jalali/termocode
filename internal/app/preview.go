package app

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/mdview"
	"github.com/amin-jalali/termocode/internal/preview"
	"github.com/amin-jalali/termocode/internal/toast"
)

// PreviewMsg carries content for the full-screen preview overlay: either
// pre-styled Body text, or a Doc that renders itself for the window width
// (the Markdown reader view).
type PreviewMsg struct {
	Title string
	Body  string
	Doc   *preview.Doc
	Badge string // a small label next to the title ("Preview"); Doc only

	help *helpPage // set when the Doc is a page of the built-in guide (help.go)
}

// markdownDoc returns a preview.Doc that renders markdown with mdview.
// Images resolve against baseDir ("" = no images).
func markdownDoc(content, baseDir string) *preview.Doc {
	var img func(dest, alt string, width int) []string
	if baseDir != "" {
		img = markdownImage(baseDir)
	}
	return preview.NewDoc(func(width int) []string {
		return mdview.Render(content, mdview.Options{Width: width, Image: img, RTL: rtlOn()})
	})
}

// openPreviewCmd fetches the active buffer's content and opens the
// Markdown reader view. Only triggered when the active file looks like
// a markdown document — non-markdown files would render as raw text
// dressed up with markdown styling, which is just visual noise. We push
// a toast in that case so the user knows why nothing happened.
func (m *Model) openPreviewCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	path := m.editor.Path()
	if !isMarkdownFile(path) {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "Markdown preview only works on .md files")
		return toastCmd
	}
	c := m.nvim
	width := m.w
	if width <= 0 {
		width = 80
	}
	return func() tea.Msg {
		content, err := c.BufferContent()
		if err != nil {
			return ErrMsg{Err: err}
		}
		doc := markdownDoc(content, filepath.Dir(path))
		doc.Lines(width) // render off the UI goroutine; View reuses it
		title := "Preview"
		if path != "" {
			title = filepath.Base(path)
		}
		return PreviewMsg{Title: title, Doc: doc, Badge: "Preview"}
	}
}

// isMarkdownFile reports whether `path` has an extension we treat as
// markdown for preview purposes. Case-insensitive on the extension.
func isMarkdownFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown", ".mdown", ".mkd", ".mdx":
		return true
	}
	return false
}

// stripANSI removes CSI escape sequences from s, for plain-text checks
// on styled rows.
func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			in = true
			i++
			continue
		}
		if in {
			if c >= 0x40 && c <= 0x7e {
				in = false
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// isMarkdownPath reports whether path looks like a markdown file.
func isMarkdownPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown", ".mdx", ".mdown":
		return true
	}
	return false
}
