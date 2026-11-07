package app

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Proof of work (CMP-52). The AI shoots pages with proof_screenshot
// (internal/ai/tools_proof.go); AI: Proof of Work… lists the shots of
// this workspace, newest first, opens one in the picture preview and
// deletes them all after a card with Cancel first.

const (
	pickerKindAIProofs      pickerKindEnum  = 5521
	confirmKindAIProofClear confirmKindEnum = 5522
)

const (
	aiProofRowOpenPrefix = "proof-open:"
	aiProofRowClear      = "proof-clear"
)

// aiProofRoot is the workspace whose proofs the picker shows (a seam).
var aiProofRoot = aiWorkspaceRoot

// aiProofTool is proof_screenshot on the workspace at root.
func aiProofTool(root string) ai.Tool {
	return ai.ProofShotTool(ai.Workspace{Root: root}, ai.FindProofBrowser)
}

// aiProofHost is the address part a row shows ("localhost:5173/settings",
// or the file name of a workspace page).
func aiProofHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return raw
	}
	if u.Scheme == "file" {
		return filepath.Base(u.Path)
	}
	return u.Host + u.Path
}

// aiProofItems are the picker rows: Delete all first, then each shot.
func aiProofItems(root string) []picker.Item {
	list, _ := ai.ListProofs(root)
	if len(list) == 0 {
		return nil
	}
	items := []picker.Item{{ID: aiProofRowClear, Title: fmt.Sprintf("Delete all %d proof screenshots…", len(list)), Hint: "asks first"}}
	for _, p := range list {
		var hint []string
		if h := aiProofHost(p.URL); h != "" {
			hint = append(hint, h)
		}
		if p.Width > 0 && p.Height > 0 {
			hint = append(hint, fmt.Sprintf("%d×%d", p.Width, p.Height))
		}
		if !p.Time.IsZero() {
			hint = append(hint, p.Time.Local().Format("Jan 2 15:04"))
		}
		if p.Note != "" {
			hint = append(hint, p.Note)
		}
		title := strings.TrimSuffix(filepath.Base(p.Path), filepath.Ext(p.Path))
		items = append(items, picker.Item{ID: aiProofRowOpenPrefix + p.Path, Title: title, Hint: strings.Join(hint, " · ")})
	}
	return items
}

// openAIProofs opens AI: Proof of Work….
func (m *Model) openAIProofs() tea.Cmd {
	root := aiProofRoot()
	items := aiProofItems(root)
	if len(items) == 0 {
		return m.aiInfo("No proof screenshots yet: the AI takes them with proof_screenshot after a UI change")
	}
	m.picker = picker.NewItems(" "+aiSpark()+" Proof of Work ", items)
	m.picker.SetNote([]string{"Screenshots the AI took in a headless browser, saved in " + ai.ProofDir + "/. Enter opens one."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIProofs
	return nil
}

// onAIProofsPicked opens a shot or asks before deleting them all.
func (m *Model) onAIProofsPicked(id string) tea.Cmd {
	switch {
	case id == aiProofRowClear:
		root := aiProofRoot()
		list, _ := ai.ListProofs(root)
		if len(list) == 0 {
			return m.aiInfo("No proof screenshots to delete")
		}
		m.confirm = confirm.Risky("Delete all proof screenshots?",
			fmt.Sprintf("Deletes %d screenshots (and their notes) from %s.\nOther files there stay.", len(list), filepath.Join(root, filepath.FromSlash(ai.ProofDir))),
			confirm.Button{ID: "cancel", Title: "Cancel"},
			confirm.Button{ID: "delete", Title: "Delete", Style: confirm.StyleDestructive})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindAIProofClear
		return nil
	case strings.HasPrefix(id, aiProofRowOpenPrefix):
		return m.openImagePreview(strings.TrimPrefix(id, aiProofRowOpenPrefix))
	}
	return nil
}

// answerAIProofClear deletes the shots on Delete; anything else keeps them.
func (m *Model) answerAIProofClear(id string) tea.Cmd {
	if id != "delete" {
		return nil
	}
	n, err := ai.ClearProofs(aiProofRoot())
	if err != nil {
		return m.aiToastErr("Could not delete the proof screenshots", err)
	}
	return m.aiInfo(fmt.Sprintf("Deleted %d proof screenshots", n))
}
