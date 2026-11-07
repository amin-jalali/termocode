package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/codeindex"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Search the codebase by meaning (CMP-69) ─────────────────────────────
//
// Search: Codebase by Meaning… asks for plain words ("where do we check
// the password") and lists the best matching chunks of the workspace's
// files; Enter jumps to the line. The AI has the same search as the
// read-only codebase_search tool. The index (internal/codeindex) is
// built here, in Go, from the git-tracked files; secret files are never
// read into it, and nothing leaves this computer.

const (
	codebaseSearchID                        = "codebase-search"
	promptKindCodebaseSearch promptKindEnum = 6969
	codebaseSearchMax                       = 30 // hits in the picker
	codebaseSearchToolMax                   = 15 // hits for the model
)

func init() {
	aiActionPolicies[codebaseSearchID] = aiNever(aiNoPicker + " (codebase_search)")
}

// codebaseSearchPaletteItems is the palette entry.
func codebaseSearchPaletteItems() []picker.Item {
	return []picker.Item{{ID: codebaseSearchID, Title: "Search: Codebase by Meaning…", Hint: "plain words, local index"}}
}

// dispatchCodebaseSearchPalette handles the palette id.
func (m *Model) dispatchCodebaseSearchPalette(id string) (tea.Cmd, bool) {
	if id != codebaseSearchID {
		return nil, false
	}
	return m.openCodebaseSearch(), true
}

// codebaseSkip is the index's secret-file rule: the same files the AI
// may never read.
func codebaseSkip(rel string) bool { return ai.SecretFile(rel) }

// openCodebaseSearch asks for the words.
func (m *Model) openCodebaseSearch() tea.Cmd {
	if _, remote := m.remoteFS(); remote {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, "Codebase search works on local folders only for now")
		return c
	}
	m.prompt = prompt.New(" Search codebase by meaning ", "Describe the code you look for:", "")
	m.prompt.SetNote("Indexed here, on this computer; secret files are left out.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindCodebaseSearch
	return nil
}

// handleCodebaseSearchPrompt routes the prompt. ok=false: not ours.
func (m *Model) handleCodebaseSearchPrompt(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindCodebaseSearch {
		return nil, false
	}
	return m.onCodebaseSearch(value), true
}

// onCodebaseSearch updates the index off the UI loop and lists the hits.
func (m *Model) onCodebaseSearch(q string) tea.Cmd {
	q = strings.TrimSpace(q)
	root, err := m.rootDir()
	if q == "" || err != nil {
		return nil
	}
	m.picker = picker.NewItems(" Codebase: "+q+" ", []picker.Item{{Header: true, Title: "Indexing and searching…", Group: "x"}})
	m.picker.SetNote([]string{"Enter opens the file at the line"})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindWorkspaceSymbols
	return func() tea.Msg {
		x, st, err := codeindex.Refresh(context.Background(), root, codebaseSkip)
		hits := x.Search(q, codebaseSearchMax)
		return applyMsg(func(m *Model) tea.Cmd {
			if !m.pickerOpen || m.pickerKind != pickerKindWorkspaceSymbols {
				return nil // closed meanwhile
			}
			items, index := codebaseHitItems(root, hits)
			m.workspaceSymIndex = index
			if len(items) == 0 {
				title := "No match. Try other words."
				if err != nil {
					title = "Index failed: " + err.Error()
				}
				items = []picker.Item{{Header: true, Title: title, Group: "x"}}
			}
			note := fmt.Sprintf("%d files, %d chunks indexed", st.Files, st.Chunks)
			if st.Capped {
				note += " (file cap reached)"
			}
			m.picker.SetNote([]string{"Enter opens the file at the line", note})
			m.picker.SetItems(items)
			return nil
		})
	}
}

// codebaseHitItems turns hits into picker rows and their jump targets.
func codebaseHitItems(root string, hits []codeindex.Hit) ([]picker.Item, map[string]workspaceSymbolTarget) {
	items := make([]picker.Item, 0, len(hits))
	index := make(map[string]workspaceSymbolTarget, len(hits))
	for i, h := range hits {
		id := fmt.Sprintf("cb-%d", i)
		title := h.Preview
		if title == "" {
			title = h.Path
		}
		items = append(items, picker.Item{ID: id, Title: title, Hint: fmt.Sprintf("%s:%d  %.0f%%", h.Path, h.Line, h.Score*100)})
		index[id] = workspaceSymbolTarget{File: filepath.Join(root, filepath.FromSlash(h.Path)), Line: h.Line}
	}
	return items, index
}

// codebaseSearchTool is the read-only AI tool over the same index.
func codebaseSearchTool(root string) ai.Tool {
	return ai.Tool{
		ReadOnly: true,
		Name:     "codebase_search",
		Description: "Find code by meaning: describe what it does in plain words (\"where the password is checked\") and get the best " +
			"matching places as file:line with a score, best first. A local index of the git-tracked files (related words count too; " +
			"secret files are left out). Use search for exact text, lsp tools for symbols.",
		Params: map[string]any{
			"query": map[string]any{"type": "string", "description": "what the code does, in plain words"},
			"max":   map[string]any{"type": "integer", "description": "number of results (default 10, max 15)"},
		},
		Required: []string{"query"},
		Describe: func(a map[string]any) string { return fmt.Sprintf("Codebase search: %q", ai.ArgString(a, "query")) },
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			q := strings.TrimSpace(ai.ArgString(a, "query"))
			if q == "" {
				return "", errors.New("query is empty")
			}
			k := ai.ArgInt(a, "max")
			if k <= 0 {
				k = 10
			}
			k = min(k, codebaseSearchToolMax)
			x, st, err := codeindex.Refresh(ctx, root, codebaseSkip)
			if err != nil {
				return "", err
			}
			hits := x.Search(q, k)
			if len(hits) == 0 {
				return "no matches (try other words, or search for exact text)", nil
			}
			var b strings.Builder
			for _, h := range hits {
				fmt.Fprintf(&b, "%s:%d: (%.2f) %s\n", h.Path, h.Line, h.Score, h.Preview)
			}
			if st.Capped {
				b.WriteString("(the index stopped at its file cap; some files are not in it)\n")
			}
			return b.String(), nil
		},
	}
}
