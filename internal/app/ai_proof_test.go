package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// aiProofSeed writes one proof screenshot (and its side file) by hand.
func aiProofSeed(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, ai.ProofDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	png := filepath.Join(dir, name+".png")
	os.WriteFile(png, []byte("\x89PNG\r\n\x1a\nfake"), 0o644)
	os.WriteFile(filepath.Join(dir, name+".json"), []byte(`{"url":"http://localhost:5173/","width":390,"height":844,"note":"phone view"}`), 0o644)
	return png
}

func aiProofModel(t *testing.T) (*Model, string) {
	t.Helper()
	m, _, root, _ := aiSkillsModel(t)
	old := aiProofRoot
	aiProofRoot = func() string { return root }
	t.Cleanup(func() { aiProofRoot = old })
	return m, root
}

// Spec (CMP-52): the chat always has proof_screenshot (it says how to
// fix a missing browser when it runs); AI: Proof of Work… lists the
// screenshots newest first with their address and size; a pick opens
// the picture; with none it says how the AI takes them.
func TestAIProofOfWorkPicker(t *testing.T) {
	m, root := aiProofModel(t)
	call := func(context.Context, aiIDEFunc) (string, error) { return "", nil }
	if _, ok := ai.FindTool(m.aiChatTools(root, call), "proof_screenshot"); !ok {
		t.Fatal("proof_screenshot missing from the chat tools")
	}
	found := false
	for _, it := range paletteItems() {
		found = found || it.ID == "ai-proofs"
	}
	if !found {
		t.Fatal("no ai-proofs palette command")
	}
	m.openAIProofs()
	if m.pickerOpen {
		t.Fatal("picker opened with no proofs")
	}

	aiProofSeed(t, root, "20261107-103000-settings")
	png := aiProofSeed(t, root, "20261107-103100-home")
	m.openAIProofs()
	if !m.pickerOpen || m.pickerKind != pickerKindAIProofs {
		t.Fatal("no proof picker")
	}
	var view strings.Builder
	for _, it := range aiProofItems(root) {
		view.WriteString(it.Title + " " + it.Hint + "\n")
	}
	for _, want := range []string{"home", "settings", "localhost:5173", "390×844"} {
		if !strings.Contains(view.String(), want) {
			t.Fatalf("picker misses %q:\n%s", want, view.String())
		}
	}
	if v := view.String(); strings.Index(v, "home") > strings.Index(v, "settings") {
		t.Fatal("not newest first")
	}
	m.pickerOpen = false
	m.onAIProofsPicked(aiProofRowOpenPrefix + png)
	if !m.imageOpen || m.imagePrev == nil || m.imagePrev.path != png {
		t.Fatal("pick did not open the picture")
	}
}

// Spec (CMP-52): Delete all asks first on a card with Cancel first;
// Cancel keeps the files, Delete removes only the proof files.
func TestAIProofClearCard(t *testing.T) {
	m, root := aiProofModel(t)
	png := aiProofSeed(t, root, "20261107-103000-settings")
	m.onAIProofsPicked(aiProofRowClear)
	if !m.confirmOpen || m.confirmKind != confirmKindAIProofClear {
		t.Fatal("no card before delete")
	}
	if m.confirm.SafeID() != "cancel" {
		t.Fatalf("safe button %q", m.confirm.SafeID())
	}
	m.answerAIProofClear("cancel")
	if _, err := os.Stat(png); err != nil {
		t.Fatal("cancel deleted the proof")
	}
	m.onAIProofsPicked(aiProofRowClear)
	m.answerAIProofClear("delete")
	if _, err := os.Stat(png); !os.IsNotExist(err) {
		t.Fatal("delete kept the proof")
	}
}

// Spec (CMP-52): the AI may not open the proof picker (it takes proofs
// with its own tool), and the web parity list names the command.
func TestAIProofPolicyAndParity(t *testing.T) {
	p, ok := aiActionPolicyFor("ai-proofs")
	if !ok || p.Gate != aiGateNever {
		t.Fatalf("policy %+v %v", p, ok)
	}
	b, err := os.ReadFile(filepath.Join("testdata", "web_parity.txt"))
	if err != nil || !strings.Contains(string(b), "\tai-proofs\t") {
		t.Fatal("ai-proofs missing from web_parity.txt")
	}
}
