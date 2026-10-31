package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// One-click servers for docs, code and web search and for browser
// checks (OMX-13, OMX-44). A pick shows the exact mcp.json entry (and,
// for the browser, the skill file) on a card with Cancel first; Add
// writes them to your own files. Nothing here holds a secret.

// aiWebPreset is one server the user can add.
type aiWebPreset struct {
	name, title, blurb string
	entry              map[string]any
	skill              string // a user skill file name written with it ("" none)
	skillText          string
}

// aiVisualQASkill is the visual-qa skill of the browser preset.
const aiVisualQASkill = `---
name: visual-qa
description: Check a web UI in a real browser (layout, text, console errors) with the browser server tools
---
Use the browser server's tools (mcp__browser__*) to check a page:

1. Open the page the user names (or the local dev server) with the navigate tool.
2. Take a snapshot of the page; read the visible text and the layout.
3. Check at a phone width (390 px) and a desktop width (1280 px): nothing
   cut off, no text over other text, no sideways scroll.
4. Read the console messages; report errors and failed requests.
5. Click the main buttons and links the task is about; check the result.
6. Report: what you checked, what is wrong (with the element and the
   width), and a screenshot path when the tool gives one.

Only read pages: do not submit forms with real data, do not sign in, do
not buy anything.
`

// aiWebPresets lists the servers of AI: Web Tools….
func aiWebPresets() []aiWebPreset {
	return []aiWebPreset{
		{name: "docs", title: "Library docs (context7)", blurb: "up-to-date docs for many libraries",
			entry: map[string]any{"type": mcp.TypeHTTP, "url": "https://mcp.context7.com/mcp"}},
		{name: "code-search", title: "Code search (grep.app)", blurb: "search code in public GitHub repos",
			entry: map[string]any{"type": mcp.TypeHTTP, "url": "https://mcp.grep.app"}},
		{name: "web-search", title: "Web search (Exa)", blurb: "web search without a key (rate limited)",
			entry: map[string]any{"type": mcp.TypeHTTP, "url": "https://mcp.exa.ai/mcp"}},
		{name: "browser", title: "Browser automation (Playwright)", blurb: "open pages, click, screenshots · needs Node",
			entry: map[string]any{"command": "npx", "args": []any{"-y", "@playwright/mcp@latest", "--isolated"}},
			skill: "visual-qa.md", skillText: aiVisualQASkill},
	}
}

func aiWebPresetByName(name string) (aiWebPreset, bool) {
	for _, p := range aiWebPresets() {
		if p.name == name {
			return p, true
		}
	}
	return aiWebPreset{}, false
}

// askAIWebPreset shows what adding preset name writes.
func (m *Model) askAIWebPreset(name string) tea.Cmd {
	p, ok := aiWebPresetByName(name)
	if !ok {
		return nil
	}
	if len(mcpServerEntries(p.name)) > 0 {
		return m.aiInfo("A server named " + p.name + " is already in mcp.json")
	}
	m.aiS().webPreset = p.name
	b, _ := json.Marshal(p.entry)
	body := "Adds to " + mcpUserPath() + ":\n    \"" + p.name + "\": " + string(b)
	if p.skill != "" {
		body += "\nand the skill " + filepath.Join(aiSkillsUserDir(), p.skill) + " (how to check a page)."
	}
	body += "\n\nIt starts when the AI first needs it. Remove it in AI: MCP Servers…."
	m.confirm = confirm.Risky("Add "+p.title+"?", body,
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "add", Title: "Add", Style: confirm.StylePrimary})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIWebAdd
	return nil
}

// answerAIWebPreset writes the entry (and skill) on Add.
func (m *Model) answerAIWebPreset(id string) tea.Cmd {
	s := m.aiS()
	name := s.webPreset
	s.webPreset = ""
	p, ok := aiWebPresetByName(name)
	if id != "add" || !ok {
		return nil
	}
	if err := mcp.AddServer(mcpUserPath(), p.name, p.entry); err != nil {
		return m.aiToastErr("Could not add the server", errors.New(ai.Redact(err.Error())))
	}
	if p.skill != "" {
		if dir := aiSkillsUserDir(); dir != "" {
			path := filepath.Join(dir, p.skill)
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				if err := os.MkdirAll(dir, 0o755); err == nil {
					_ = os.WriteFile(path, []byte(p.skillText), 0o644)
				}
			}
		}
	}
	m.aiMCP().Reload()
	return m.aiInfo(p.title + " added: the AI can use it from the next message")
}
