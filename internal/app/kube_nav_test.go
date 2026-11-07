package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func kubeKey(m *Model, key string) {
	k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	mm, _ := m.handleKubeSidebarKey(k)
	*m = mm
}

func TestKubeBackForwardWalksTheScopes(t *testing.T) {
	_, m := kubeLoaded(t, 100, 30)
	st := m.kube
	if st.context != "dev" || st.namespace != "shop" {
		t.Fatalf("scope = %s/%s", st.context, st.namespace)
	}
	m.kubeRescope("dev", "billing")
	m.kubeRescope("prod", "web")
	kubeKey(m, "[")
	if st.context != "dev" || st.namespace != "billing" {
		t.Fatalf("back 1 = %s/%s", st.context, st.namespace)
	}
	kubeKey(m, "[")
	if st.context != "dev" || st.namespace != "shop" {
		t.Fatalf("back 2 = %s/%s", st.context, st.namespace)
	}
	kubeKey(m, "[") // nothing older: stays
	if st.namespace != "shop" {
		t.Fatalf("back past the start = %s/%s", st.context, st.namespace)
	}
	kubeKey(m, "]")
	if st.namespace != "billing" {
		t.Fatalf("forward = %s/%s", st.context, st.namespace)
	}
	// A new place drops the forward list.
	m.kubeRescope("dev", "ops")
	kubeKey(m, "]")
	if st.namespace != "ops" {
		t.Fatalf("forward after a new place = %s/%s", st.context, st.namespace)
	}
	kubeKey(m, "[")
	if st.namespace != "billing" {
		t.Fatalf("back after a new place = %s/%s", st.context, st.namespace)
	}
}

func TestKubeScreenText(t *testing.T) {
	_, m := kubeLoaded(t, 100, 30)
	st := m.kube
	if len(st.pods) == 0 {
		t.Fatal("fixture has no pods")
	}
	txt := kubeScreenText(st)
	for _, want := range []string{"Kubernetes  dev / shop", "PODS (" + itoa(len(st.pods)) + ")", "  " + st.pods[0].Name, "DEPLOYMENTS"} {
		if !strings.Contains(txt, want) {
			t.Errorf("screen text lacks %q:\n%s", want, txt)
		}
	}
}

func TestKubeSaveScreen(t *testing.T) {
	_, m := kubeLoaded(t, 100, 30)
	kubeKey(m, "w")
	if !m.promptOpen || m.promptKind != promptKindKubeSaveScreen || !strings.HasSuffix(m.prompt.Value(), ".txt") {
		t.Fatalf("no save prompt: open=%v kind=%d value=%q", m.promptOpen, m.promptKind, m.prompt.Value())
	}
	path := filepath.Join(t.TempDir(), "screen.txt")
	m.kubeSaveScreen(path)
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "dev / shop") {
		t.Fatalf("saved = %q %v", b, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
	// An existing file is never overwritten.
	if err := os.WriteFile(path, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.kubeSaveScreen(path)
	if b, _ := os.ReadFile(path); string(b) != "mine" {
		t.Fatalf("overwrote: %q", b)
	}
}
