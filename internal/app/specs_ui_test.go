package app

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/specs"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// specModel is an AI test model in a local project folder.
func specModel(t *testing.T) (*Model, string) {
	t.Helper()
	m := aiTestModel(t)
	root := t.TempDir()
	m.ws = workspace.Workspace{ID: workspace.LocalID(root), Kind: workspace.KindLocal, Root: root}
	return &m, root
}

func specNow() time.Time { return time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC) }

func specAnswer(m *Model, id string) {
	m.confirmOpen = false
	m.handleConfirmSelect(id)
}

// The Spec palette rows exist and the AI can never run them: phases are
// approved by the user only.
func TestSpecPaletteAndPolicy(t *testing.T) {
	ids := map[string]bool{}
	for _, it := range specPaletteItems() {
		ids[it.ID] = true
	}
	for _, id := range []string{specNewPaletteID, specNewBugPaletteID, specOpenPaletteID} {
		if !ids[id] {
			t.Errorf("palette lacks %s", id)
		}
		if p := aiActionPolicies[id]; p.Gate != aiGateNever {
			t.Errorf("%s gate %q", id, p.Gate)
		}
	}
}

// Spec: New… asks for the goal, makes the spec folder and shows its
// card: Close first, the current phase, Ask AI and Open; nothing is
// approved by opening it.
func TestSpecNewAndCard(t *testing.T) {
	m, root := specModel(t)
	m.dispatchSpecPalette(specNewPaletteID)
	if !m.promptOpen || m.promptKind != promptKindSpecNew {
		t.Fatal("no goal prompt")
	}
	m.promptOpen = false
	if _, ok := m.handleSpecPromptSubmit("Add a dark mode toggle"); !ok {
		t.Fatal("the prompt was not ours")
	}
	s, err := specs.Load(root, "add-a-dark-mode-toggle")
	if err != nil {
		t.Fatal(err)
	}
	if !m.confirmOpen || m.confirmKind != confirmKindSpecCard || m.confirm.SafeID() != "close" {
		t.Fatalf("no spec card (kind %d safe %q)", m.confirmKind, m.confirm.SafeID())
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"Add a dark mode toggle", "Requirements", "Design", "Tasks", "Build", "Review"} {
		if !strings.Contains(box, want) {
			t.Errorf("card lacks %q:\n%s", want, box)
		}
	}
	// Ask AI: the phase prompt waits in the chat input; nothing is sent.
	specAnswer(m, "draft")
	in := string(m.aiS().input)
	if !strings.Contains(in, "requirements.md") || m.aiS().streaming {
		t.Errorf("chat input %q streaming %v", in, m.aiS().streaming)
	}
	if s.Status(specs.Requirements) == specs.Approved {
		t.Error("opening approved a phase")
	}
}

// Approve…: a Cancel-first card; Cancel approves nothing, Approve binds
// the phase to the file and the card moves on; a template cannot be
// approved.
func TestSpecApprove(t *testing.T) {
	m, root := specModel(t)
	s, err := specs.Create(root, "pin row", specs.Feature, specNow())
	if err != nil {
		t.Fatal(err)
	}
	m.openSpecCard(s.Name)
	specAnswer(m, "approve")
	if m.confirmKind == confirmKindSpecApprove {
		t.Fatal("an empty template got an approve card")
	}
	if err := os.WriteFile(s.FilePath(specs.Requirements), []byte("# R\n- WHEN x THEN y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.openSpecCard(s.Name)
	specAnswer(m, "approve")
	if !m.confirmOpen || m.confirmKind != confirmKindSpecApprove || m.confirm.SafeID() != "cancel" {
		t.Fatalf("approve card kind %d safe %q", m.confirmKind, m.confirm.SafeID())
	}
	specAnswer(m, "cancel")
	if re, _ := specs.Load(root, s.Name); re.Status(specs.Requirements) == specs.Approved {
		t.Fatal("Cancel approved")
	}
	m.openSpecCard(s.Name)
	specAnswer(m, "approve")
	specAnswer(m, "approve")
	re, _ := specs.Load(root, s.Name)
	if re.Status(specs.Requirements) != specs.Approved {
		t.Fatal("not approved")
	}
	if p, _ := re.Current(); p != specs.Design || !m.confirmOpen || m.confirmKind != confirmKindSpecCard {
		t.Errorf("after approve: current %v card %v", p, m.confirmOpen)
	}
}

// Spec: Open… lists the specs with their phase; a pick opens its card;
// no spec says how to start one.
func TestSpecOpenPicker(t *testing.T) {
	m, root := specModel(t)
	m.dispatchSpecPalette(specOpenPaletteID)
	if m.pickerOpen || !strings.Contains(covToastText(m), "Spec: New") {
		t.Fatalf("no specs: picker %v toast %q", m.pickerOpen, covToastText(m))
	}
	if _, err := specs.Create(root, "pin row", specs.Feature, specNow()); err != nil {
		t.Fatal(err)
	}
	m.dispatchSpecPalette(specOpenPaletteID)
	if !m.pickerOpen || m.pickerKind != pickerKindSpecOpen {
		t.Fatal("no picker")
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "pin-row"})
	if !m.confirmOpen || m.confirmKind != confirmKindSpecCard {
		t.Error("the pick did not open the card")
	}
}

// A spec in a remote or container workspace is refused for now.
func TestSpecLocalOnly(t *testing.T) {
	m, _ := specModel(t)
	m.ws = workspace.Workspace{ID: "container:x", Kind: workspace.KindContainer, Root: "/app"}
	m.dispatchSpecPalette(specNewPaletteID)
	if m.promptOpen {
		t.Error("a goal prompt in a container workspace")
	}
}
