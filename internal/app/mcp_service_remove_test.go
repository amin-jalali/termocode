package app

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// Spec: Remove… asks first. Keep is the first button and changes
// nothing; Remove deletes the entry, its stored sign-in and the secrets
// only it uses, and stops the running server.
func TestMCPServiceRemove(t *testing.T) {
	m := aiTestModel(t)
	marker := filepath.Join(t.TempDir(), "running")
	body := mcptest.Config(map[string]map[string]string{
		"fake":  {"FAKE_MARKER": marker, "TOKEN": "${secret:fake_token}", "SHARED": "${secret:shared}"},
		"other": {"SHARED": "${secret:shared}"},
	})
	writeUserMCP(t, body)
	cs := mcp.CredStore{Path: mcpCredPath()}
	_ = cs.SetSecret("fake_token", "s3cret-value-aaaa")
	_ = cs.SetSecret("shared", "shared-value-bbbb")
	_ = cs.Save("fake", ai.MCPOAuth{Resource: "https://x.example/mcp", AccessToken: "at-1234567890"})
	mgr := m.aiMCP()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := mgr.Connect(ctx, nil, "fake"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { _, err := os.Stat(marker); return err == nil })

	m.aiS().mcpSel = "fake"
	m.onMCPServerAction("remove")
	if !m.confirmOpen || m.confirmKind != confirmKindMCPRemove {
		t.Fatalf("no remove card: open=%v kind=%v", m.confirmOpen, m.confirmKind)
	}
	bs := m.confirm.Buttons()
	if len(bs) != 2 || bs[0].ID != "keep" || bs[0].Style == confirm.StyleDestructive ||
		bs[1].ID != "remove" || bs[1].Style != confirm.StyleDestructive {
		t.Fatalf("buttons %+v", bs)
	}

	// Keep (and Esc): nothing changes.
	m.confirmOpen = false
	runMCPCmd(m.answerMCPRemove("keep"))
	if b, _ := os.ReadFile(mcpUserPath()); string(b) != body {
		t.Fatalf("Keep changed mcp.json: %s", b)
	}
	if _, ok := cs.Load("fake"); !ok {
		t.Error("Keep dropped the sign-in")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("Keep stopped the server")
	}

	// Remove.
	m.aiS().mcpSel = "fake"
	m.onMCPServerAction("remove")
	m.confirmOpen = false
	runMCPCmd(m.answerMCPRemove("remove"))
	if es := mcpServerEntries("fake"); len(es) != 0 {
		t.Errorf("entry still in mcp.json: %+v", es)
	}
	if len(mcpServerEntries("other")) != 1 {
		t.Error("the other service went too")
	}
	if _, ok := cs.Load("fake"); ok {
		t.Error("sign-in kept")
	}
	cr, _ := ai.LoadCredentials(mcpCredPath())
	if _, ok := cr.MCPSecrets["fake_token"]; ok {
		t.Error("its own secret kept")
	}
	if cr.MCPSecrets["shared"] == "" {
		t.Error("a secret another service uses was deleted")
	}
	waitFor(t, func() bool { _, err := os.Stat(marker); return err != nil })
	if st := serverStatus(&m, "fake"); st.Name != "" {
		t.Errorf("still listed: %+v", st)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func runMCPCmd(c tea.Cmd) {
	if c != nil {
		c()
	}
}
