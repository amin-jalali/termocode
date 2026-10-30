package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Chat → file (Group L2): "Save as…" on a code block writes it to a
// workspace file (asks for the path, confirms an overwrite, then opens
// the file), and "AI: Export Chat as Markdown" writes the chat to
// ai-chat-<date>.md in the workspace and opens it.

// Own bases: no clash with the other AI kinds (ai_config_ui.go).
const promptKindAISaveAs promptKindEnum = 960
const confirmKindAISaveOverwrite confirmKindEnum = 960

// aiSaveLangExt maps a fence language to a file extension.
var aiSaveLangExt = map[string]string{
	"go": "go", "python": "py", "py": "py", "javascript": "js", "js": "js", "jsx": "jsx",
	"typescript": "ts", "ts": "ts", "tsx": "tsx", "rust": "rs", "rs": "rs",
	"sh": "sh", "bash": "sh", "shell": "sh", "zsh": "sh", "console": "sh",
	"json": "json", "yaml": "yaml", "yml": "yaml", "toml": "toml", "ini": "ini",
	"markdown": "md", "md": "md", "html": "html", "css": "css", "scss": "scss",
	"c": "c", "h": "h", "cpp": "cpp", "c++": "cpp", "java": "java", "kotlin": "kt", "kt": "kt",
	"lua": "lua", "ruby": "rb", "rb": "rb", "php": "php", "sql": "sql", "swift": "swift",
	"xml": "xml", "diff": "diff", "patch": "diff", "vim": "vim", "dart": "dart", "zig": "zig",
	"text": "txt", "txt": "txt", "makefile": "mk", "dockerfile": "dockerfile",
}

// aiPathWordRe matches a word that looks like a file path with an
// extension ("cmd/main.go", "README.md").
var aiPathWordRe = lazyre.New(`^[\w./-]*[\w-]\.[A-Za-z][A-Za-z0-9]*$`)

// aiCodeBlockInfo returns the n-th fenced block of a message with its
// language and a file name hint: a path in the fence line
// ("```go title=main.go", "```main.go") or the last path named in the
// line just above the block ("Put this in `cmd/main.go`:").
func aiCodeBlockInfo(text string, n int) (code, lang, hint string, ok bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	k := -1
	for i := 0; i < len(lines); i++ {
		trim := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trim, "```") {
			continue
		}
		k++
		start := i
		var body []string
		for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
			body = append(body, lines[i])
		}
		if k != n {
			continue
		}
		info := strings.Fields(strings.TrimPrefix(trim, "```"))
		if len(info) > 0 {
			lang = strings.ToLower(info[0])
		}
		for _, w := range info {
			w = strings.Trim(strings.TrimPrefix(strings.TrimPrefix(w, "title="), "file="), `"'`)
			if aiPathWordRe.MatchString(w) && !strings.HasPrefix(w, ".") {
				hint = w
				if w == info[0] {
					lang = strings.ToLower(strings.TrimPrefix(filepath.Ext(w), "."))
				}
			}
		}
		if hint == "" {
			for j := start - 1; j >= 0; j-- {
				if strings.TrimSpace(lines[j]) == "" {
					continue
				}
				hint = aiPathInLine(lines[j])
				break
			}
		}
		return strings.Join(body, "\n"), lang, hint, true
	}
	return "", "", "", false
}

// aiPathInLine finds the last file path named in a prose line: a
// `backticked` path first, else a plain word with a known extension.
func aiPathInLine(line string) string {
	found := ""
	parts := strings.Split(line, "`")
	for i := 1; i < len(parts); i += 2 {
		if w := strings.TrimSpace(parts[i]); aiPathWordRe.MatchString(w) {
			found = w
		}
	}
	if found != "" {
		return found
	}
	for _, w := range strings.Fields(line) {
		w = strings.TrimRight(w, ".,;:!?)*_\"'")
		w = strings.TrimLeft(w, "(*_\"'")
		if !aiPathWordRe.MatchString(w) {
			continue
		}
		if ext := strings.TrimPrefix(filepath.Ext(w), "."); aiSaveLangExt[strings.ToLower(ext)] != "" {
			found = w
		}
	}
	return found
}

// aiSaveAsName is the default "Save as…" path for a block.
func aiSaveAsName(lang, hint string) string {
	if hint != "" {
		return hint
	}
	ext := aiSaveLangExt[strings.ToLower(lang)]
	if ext == "" {
		ext = "txt"
	}
	return "snippet." + ext
}

// aiSaveCodeBlockAs is the "Save as…" button ("save:<msg>:<block>"): it
// asks for the path.
func (m *Model) aiSaveCodeBlockAs(id string) tea.Cmd {
	parts := strings.Split(id, ":")
	s := m.aiS()
	if len(parts) != 3 {
		return nil
	}
	i, err1 := strconv.Atoi(parts[1])
	block, err2 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || i < 0 || i >= len(s.conv.Messages) {
		return nil
	}
	code, lang, hint, ok := aiCodeBlockInfo(s.conv.Messages[i].Text, block)
	if !ok {
		return nil
	}
	s.saveCode = code
	m.prompt = prompt.New("Save code block as", "Path in the workspace:", aiSaveAsName(lang, hint))
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindAISaveAs
	return nil
}

// aiInWorkspace resolves a typed path against the workspace root.
// ok=false when it points outside the workspace.
func aiInWorkspace(path, root string) (string, bool) {
	p := strings.TrimSpace(path)
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p = filepath.Clean(p)
	r, err := filepath.Rel(root, p)
	if err != nil || r == "." || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	return p, true
}

// onAISaveAsPath is the promptKindAISaveAs submit.
func (m *Model) onAISaveAsPath(value string) tea.Cmd {
	root := aiWorkspaceRoot()
	abs, ok := aiInWorkspace(value, root)
	if !ok {
		return m.aiToastErr("Not saved", fmt.Errorf("%s is outside the workspace", strings.TrimSpace(value)))
	}
	fi, err := os.Stat(abs)
	if err == nil && fi.IsDir() {
		return m.aiToastErr("Not saved", fmt.Errorf("%s is a folder", aiRel(root)(abs)))
	}
	if err == nil {
		m.aiS().savePath = abs
		m.confirm = confirm.Risky("Overwrite?", aiRel(root)(abs)+" already exists. Replace it with the code block?",
			confirm.Button{ID: "cancel", Title: "Cancel"},
			confirm.Button{ID: "overwrite", Title: "Overwrite", Style: confirm.StyleDestructive})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindAISaveOverwrite
		return nil
	}
	return m.aiWriteAndOpen(abs, m.aiS().saveCode, "Saved")
}

// onAISaveOverwrite is the overwrite confirm.
func (m *Model) onAISaveOverwrite(id string) tea.Cmd {
	s := m.aiS()
	abs := s.savePath
	s.savePath = ""
	if id != "overwrite" || abs == "" {
		return nil
	}
	return m.aiWriteAndOpen(abs, s.saveCode, "Saved")
}

// aiWriteAndOpen writes content to abs (refusing an open tab with
// unsaved edits), refreshes the explorer and opens the file.
func (m *Model) aiWriteAndOpen(abs, content, verb string) tea.Cmd {
	root := aiWorkspaceRoot()
	rel := aiRel(root)(abs)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	_, syncCmd, err := m.aiWriteFile(abs, rel, content)
	if err != nil {
		return m.aiToastErr("Not saved", err)
	}
	m.aiS().saveCode = ""
	if m.nvim != nil {
		m.ensureEditorWindowCurrent()
		_ = m.nvim.OpenFile("edit", abs)
	}
	m.explorer.RevealPath(abs)
	m.aiS().focused = false
	m.focus = FocusEditor
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, verb+" "+rel)
	return tea.Batch(syncCmd, c)
}

// aiChatMarkdown renders a chat as a Markdown document.
func aiChatMarkdown(c ai.Conversation) string {
	var b strings.Builder
	title := strings.TrimSpace(c.Title)
	if title == "" {
		title = "AI chat"
	}
	b.WriteString("# " + title + "\n")
	for _, msg := range c.Messages {
		switch msg.Role {
		case ai.StoredUser:
			b.WriteString("\n## You\n\n" + strings.TrimSpace(msg.Text) + "\n")
			if labels := aiContextLabels(msg.Context); len(labels) > 0 {
				b.WriteString("\n_Attached: " + strings.Join(labels, ", ") + "_\n")
			}
		case ai.StoredAssistant:
			if strings.TrimSpace(msg.Text) != "" {
				b.WriteString("\n## AI\n\n" + strings.TrimSpace(msg.Text) + "\n")
			}
		case ai.StoredTool:
			b.WriteString("\n> " + strings.ReplaceAll(strings.TrimSpace(msg.Text), "\n", "\n> ") + "\n")
		case ai.StoredError:
			b.WriteString("\n> ⚠ " + strings.ReplaceAll(strings.TrimSpace(msg.Text), "\n", "\n> ") + "\n")
		}
	}
	return b.String()
}

// aiContextAttachRe finds the "Attached x:" / "Attached image x (…)."
// heads of a user message's attachment context.
var aiContextAttachRe = lazyre.New(`(?m)^Attached (?:image )?(.+?)(?::| \(sent as an image\)\.)$`)

func aiContextLabels(ctx string) []string {
	var out []string
	for _, mm := range aiContextAttachRe.FindAllStringSubmatch(ctx, -1) {
		out = append(out, mm[1])
	}
	return out
}

// aiExportChat writes the open chat to ai-chat-<date>.md (a free name)
// in the workspace and opens it.
func (m *Model) aiExportChat() tea.Cmd {
	s := m.aiS()
	if len(s.conv.Messages) == 0 {
		return m.aiInfo("Nothing to export: the chat is empty")
	}
	root := aiWorkspaceRoot()
	base := "ai-chat-" + clock.Now().Format("2006-01-02")
	abs := filepath.Join(root, base+".md")
	for n := 2; ; n++ {
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			break
		}
		abs = filepath.Join(root, fmt.Sprintf("%s-%d.md", base, n))
	}
	return m.aiWriteAndOpen(abs, aiChatMarkdown(s.conv), "Exported chat to")
}
