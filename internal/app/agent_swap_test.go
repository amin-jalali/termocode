package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentswap"
)

// AGX-14: Agent: Switch Account… moves the CLI's own sign-in file in
// place. It is off until agent_account_swap is true in config.json.

func swapModel(t *testing.T, on bool) (*Model, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := covMCPModel(t)
	if on {
		if err := os.MkdirAll(filepath.Dir(mcpConfigPath()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mcpConfigPath(), []byte(`{"agent_account_swap":true}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return m, home
}

func TestAgentSwapIsOffByDefault(t *testing.T) {
	m, _ := swapModel(t, false)
	cmd, ok := m.dispatchGitxPalette(agentSwapPaletteID)
	if !ok {
		t.Fatal("the palette command is missing")
	}
	_ = cmd
	if m.pickerOpen {
		t.Fatal("the account list opened while agent_account_swap is off")
	}
	if !strings.Contains(covToastText(m), "agent_account_swap") {
		t.Fatalf("the toast does not say how to turn it on: %q", covToastText(m))
	}
}

func TestAgentSwapCaptureUseAndUndoOnACard(t *testing.T) {
	m, home := swapModel(t, true)
	cred := filepath.Join(home, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(cred), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cred, []byte(`{"tok":"AAA-SECRET"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	vault := fakeSwapVault{}
	m.aiS().swapVault = vault
	m.dispatchGitxPalette(agentSwapPaletteID)
	if !m.pickerOpen || !strings.Contains(pickerIDs(m), "cli-codex") {
		t.Fatalf("CLI rows: %s", pickerIDs(m))
	}
	m.gitx.onPick(m, "cli-codex")
	if !strings.Contains(pickerIDs(m), "capture") {
		t.Fatalf("actions: %s", pickerIDs(m))
	}
	m.gitx.onPick(m, "capture")
	if !m.promptOpen {
		t.Fatal("no name prompt")
	}
	m.gitx.onPrompt(m, "work")
	if len(vault) != 1 {
		t.Fatalf("vault %v", vault)
	}
	// Another account is now signed in; switch back to "work" through the card.
	if err := os.WriteFile(cred, []byte(`{"tok":"BBB-OTHER"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m.dispatchGitxPalette(agentSwapPaletteID)
	m.gitx.onPick(m, "cli-codex")
	if !strings.Contains(pickerIDs(m), "use-work") {
		t.Fatalf("saved accounts: %s", pickerIDs(m))
	}
	m.gitx.onPick(m, "use-work")
	if !m.confirmOpen {
		t.Fatal("no card")
	}
	card := cardText(m)
	if !strings.Contains(card, "auth.json") || !strings.Contains(card, "hidden") || strings.Contains(card, "AAA-SECRET") || strings.Contains(card, "BBB-OTHER") {
		t.Fatalf("card: %s", card)
	}
	if b := m.confirm.Buttons(); b[0].ID != "cancel" {
		t.Fatalf("Cancel is not first: %+v", b)
	}
	m.gitx.onConfirm(m, "cancel")
	if b, _ := os.ReadFile(cred); !strings.Contains(string(b), "BBB-OTHER") {
		t.Fatal("cancel changed the file")
	}
	m.gitx.onPick(m, "use-work")
	m.gitx.onConfirm(m, "apply")
	if b, _ := os.ReadFile(cred); !strings.Contains(string(b), "AAA-SECRET") {
		t.Fatalf("file after apply: %s", b)
	}
	if _, err := os.Stat(cred + agentswap.BackupSuffix); err != nil {
		t.Fatal("no backup")
	}
}

type fakeSwapVault map[string]string

func (v fakeSwapVault) Get(n string) (string, bool, error) { s, ok := v[n]; return s, ok, nil }
func (v fakeSwapVault) Set(n, s string) error              { v[n] = s; return nil }
func (v fakeSwapVault) Delete(n string) error              { delete(v, n); return nil }
