package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/termimg"
	"github.com/amin-jalali/termocode/internal/toast"
)

// File → chat (Group L1). Files reach the chat as attachment chips in
// three ways:
//
//   - drag and drop / paste: a terminal pastes a dropped file as its path
//     (quoted, backslash-escaped or as a file:// URI, several separated
//     by spaces or new lines). A paste into the focused chat input that is
//     only existing file paths becomes chips instead of text.
//   - the attach button in the input box's bottom border opens the file
//     picker.
//   - "@path" (or "@file path") in the message attaches that workspace
//     file when the message is sent.
//
// Text files are attached as text (cut at aiMaxAttach). Images are sent
// as images when the provider takes image input, else as a short note.
// Other binary files are refused with a toast.

const (
	aiMaxAttach = 60 * 1024       // text attachment cap (bytes)
	aiMaxImage  = 5 * 1024 * 1024 // image cap (Anthropic's per-image limit)
)

// pickerKindAIAttach is the "attach file" picker (own base: no clash
// with the other AI kinds).
const pickerKindAIAttach pickerKindEnum = 960

// aiDroppedPaths reads a paste as dropped file paths. ok=false when the
// paste is not only paths of existing files (then it is plain text).
// Relative paths resolve against root.
func aiDroppedPaths(s, root string) ([]string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	// One path, as pasted (a terminal may not escape spaces).
	if p, ok := aiResolveDropped(s, root); ok {
		return []string{p}, true
	}
	all := func(toks []string) ([]string, bool) {
		if len(toks) == 0 {
			return nil, false
		}
		out := make([]string, 0, len(toks))
		for _, t := range toks {
			p, ok := aiResolveDropped(t, root)
			if !ok {
				return nil, false
			}
			out = append(out, p)
		}
		return out, true
	}
	// One path per line.
	if lines := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }); len(lines) > 1 {
		var toks []string
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			if one, err := aiShellSplit(l); err == nil && len(one) == 1 {
				l = one[0]
			}
			toks = append(toks, l)
		}
		if out, ok := all(toks); ok {
			return out, true
		}
	}
	// Shell words: 'a b' "c" d\ e.
	if toks, err := aiShellSplit(s); err == nil {
		return all(toks)
	}
	return nil, false
}

// aiResolveDropped turns one pasted word into the absolute path of an
// existing regular file.
func aiResolveDropped(tok, root string) (string, bool) {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return "", false
	}
	if rest, ok := strings.CutPrefix(tok, "file://"); ok {
		rest = strings.TrimPrefix(rest, "localhost")
		p, err := url.PathUnescape(rest)
		if err != nil {
			return "", false
		}
		tok = p
	}
	if rest, ok := strings.CutPrefix(tok, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			tok = filepath.Join(home, rest)
		}
	}
	if !filepath.IsAbs(tok) {
		tok = filepath.Join(root, tok)
	}
	tok = filepath.Clean(tok)
	fi, err := os.Stat(tok)
	if err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	return tok, true
}

// aiShellSplit splits s into words like a POSIX shell: single quotes,
// double quotes (with \" and \\) and backslash escapes.
func aiShellSplit(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	in := false // a word is open (even an empty quoted one)
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\'':
			in = true
			j := i + 1
			for j < len(rs) && rs[j] != '\'' {
				cur.WriteRune(rs[j])
				j++
			}
			if j >= len(rs) {
				return nil, errors.New("open quote")
			}
			i = j
		case r == '"':
			in = true
			j := i + 1
			for ; j < len(rs) && rs[j] != '"'; j++ {
				if rs[j] == '\\' && j+1 < len(rs) && (rs[j+1] == '"' || rs[j+1] == '\\') {
					j++
				}
				cur.WriteRune(rs[j])
			}
			if j >= len(rs) {
				return nil, errors.New("open quote")
			}
			i = j
		case r == '\\' && i+1 < len(rs):
			in = true
			i++
			cur.WriteRune(rs[i])
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if in {
				out = append(out, cur.String())
				cur.Reset()
				in = false
			}
		default:
			in = true
			cur.WriteRune(r)
		}
	}
	if in {
		out = append(out, cur.String())
	}
	return out, nil
}

// aiImageType is the media type of an image path ("" when not an image
// the providers take).
func aiImageType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return ""
}

// aiLooksBinary reports whether b (the start of a file) is not text.
func aiLooksBinary(b []byte) bool {
	if len(b) > 8000 {
		b = b[:8000]
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return true
	}
	// The sample may end inside a multi-byte rune.
	for i := 0; i < 3 && len(b) > 0 && !utf8.Valid(b); i++ {
		b = b[:len(b)-1]
	}
	return !utf8.Valid(b)
}

// aiAttachLabel is the chip label: workspace-relative, else the base name.
func aiAttachLabel(abs, root string) string {
	if r, err := filepath.Rel(root, abs); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return filepath.Base(abs)
}

// aiLoadAttachment reads one file for the chat. images says whether the
// provider takes image input. Binary files (not images) are refused.
func aiLoadAttachment(abs, root string, images bool) (aiAttachment, error) {
	label := aiAttachLabel(abs, root)
	if mt := aiImageType(abs); mt != "" && termimg.IsImagePath(abs) {
		fi, err := os.Stat(abs)
		if err != nil {
			return aiAttachment{}, err
		}
		if fi.Size() > aiMaxImage {
			return aiAttachment{}, fmt.Errorf("image is over %d MB", aiMaxImage>>20)
		}
		if !images {
			return aiAttachment{Label: label, Text: "image: " + abs + " (not sent: provider has no image input)"}, nil
		}
		return aiAttachment{Label: label, Image: abs, MediaType: mt}, nil
	}
	f, err := os.Open(abs)
	if err != nil {
		return aiAttachment{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, aiMaxAttach+1))
	if err != nil {
		return aiAttachment{}, err
	}
	if aiLooksBinary(data) {
		return aiAttachment{}, errors.New("binary file (only text and images can be attached)")
	}
	text := string(data)
	if len(data) > aiMaxAttach {
		text = strings.ToValidUTF8(text[:aiMaxAttach], "") + "\n… [truncated]"
	}
	return aiAttachment{Label: label, Text: text}, nil
}

// aiAttachPaths adds files as chips; refused ones get a warning toast.
func (m *Model) aiAttachPaths(paths []string) tea.Cmd {
	s := m.aiS()
	root := aiWorkspaceRoot()
	images := s.provider != nil && ai.SupportsImages(s.provider)
	var cmds []tea.Cmd
	var added []string
	for _, p := range paths {
		at, err := aiLoadAttachment(p, root, images)
		if err != nil {
			var c tea.Cmd
			m.toast, c = m.toast.PushDetail(toast.Warn, "Not attached: "+filepath.Base(p), err.Error())
			cmds = append(cmds, c)
			continue
		}
		dup := false
		for _, old := range s.attachments {
			if old.Label == at.Label {
				dup = true
			}
		}
		if !dup {
			s.attachments = append(s.attachments, at)
			added = append(added, at.Label)
		}
	}
	if len(added) > 0 && len(cmds) == 0 {
		cmds = append(cmds, m.aiInfo("Attached "+strings.Join(added, ", ")))
	}
	return tea.Batch(cmds...)
}

// aiMentionRe finds "@path" and "@file path" words.
var aiMentionRe = regexp.MustCompile(`(?:^|\s)@(?:file\s+)?(\S+)`)

// aiMentionedPaths lists the workspace files named with "@" in text
// (absolute, in order, without repeats). Words that are not an existing
// file inside root are skipped.
func aiMentionedPaths(text, root string) []string {
	var out []string
	seen := map[string]bool{}
	for _, mm := range aiMentionRe.FindAllStringSubmatch(text, -1) {
		word := mm[1]
		for _, cand := range []string{word, strings.TrimRight(word, ".,;:!?)\"'`")} {
			p := cand
			if !filepath.IsAbs(p) {
				p = filepath.Join(root, p)
			}
			p = filepath.Clean(p)
			if r, err := filepath.Rel(root, p); err != nil || strings.HasPrefix(r, "..") {
				continue
			}
			if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
				if !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
				break
			}
		}
	}
	return out
}

// aiAttachMentions attaches the "@" files of text that are not chips yet.
func (m *Model) aiAttachMentions(text string) tea.Cmd {
	root := aiWorkspaceRoot()
	var todo []string
	for _, p := range aiMentionedPaths(text, root) {
		label := aiAttachLabel(p, root)
		dup := false
		for _, at := range m.aiS().attachments {
			if at.Label == label {
				dup = true
			}
		}
		if !dup {
			todo = append(todo, p)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	return m.aiAttachPaths(todo)
}

// aiHistoryImages loads stored image paths for a request. Files that are
// gone (or too big) are named in the returned note instead.
func aiHistoryImages(paths []string) ([]ai.Image, string) {
	var imgs []ai.Image
	var missing []string
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil || fi.Size() > aiMaxImage {
			missing = append(missing, p)
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			missing = append(missing, p)
			continue
		}
		imgs = append(imgs, ai.Image{MediaType: aiImageType(p), Data: data})
	}
	note := ""
	if len(missing) > 0 {
		note = "(image no longer available: " + strings.Join(missing, ", ") + ")"
	}
	return imgs, note
}

// ── Attach button + file picker ────────────────────────────────────────

// aiAttachButton is the attach button in the input box's bottom border:
// panel-local columns [x0, x1) and its label. ok=false when the panel is
// too narrow.
func aiAttachButton(width int) (x0, x1 int, label string, ok bool) {
	label = " " + aiAttachGlyph() + " "
	bw := len([]rune(label)) // the glyphs are one cell wide
	inner := aiInputTextW(width) + 2
	if width < 8 || inner < bw+4 {
		return 0, 0, "", false
	}
	x1 = width - 3 // then "─╯" and the panel margin
	return x1 - bw, x1, label, true
}

// openAIAttachPicker lists the workspace files to attach one.
func (m *Model) openAIAttachPicker() tea.Cmd {
	items := loadFiles()
	if len(items) == 0 {
		return m.aiInfo("No files in workspace")
	}
	m.picker = picker.NewItems(" Attach File to AI Chat ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIAttach
	return nil
}

// onAIAttachPicked attaches the picked file and gives the chat the keys.
func (m *Model) onAIAttachPicked(id string) tea.Cmd {
	p := id
	if !filepath.IsAbs(p) {
		p = filepath.Join(aiWorkspaceRoot(), p)
	}
	cmd := m.aiAttachPaths([]string{p})
	m.openAIPanel(true)
	return cmd
}
