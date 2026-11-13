package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const kcStart = "apiVersion: v1\nclusters:\n- cluster:\n    server: https://old:6443\n  name: old\ncontexts:\n- context:\n    cluster: old\n  name: old\ncurrent-context: old\nkind: Config\n"

// kubeconfigSandbox points KUBECONFIG (and HOME) at a temp dir: no test
// ever touches the real ~/.kube.
func kubeconfigSandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	p := filepath.Join(dir, "kubeconfig")
	t.Setenv("KUBECONFIG", p)
	if err := os.WriteFile(p, []byte(kcStart), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestKubeAddClusterPaletteAndPolicy(t *testing.T) {
	found := false
	for _, it := range kubePaletteItems() {
		if it.ID == "kubernetes-add-cluster" {
			found = true
		}
	}
	if !found {
		t.Fatal("no palette row kubernetes-add-cluster")
	}
	if p, ok := aiActionPolicies["kubernetes-add-cluster"]; !ok || p.Gate != aiGateNever {
		t.Errorf("policy = %+v", p)
	}
}

func TestKubeAddClusterWritesOnlyAfterOKWithABackup(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	p := kubeconfigSandbox(t) // after kubeLoaded: it sets KUBECONFIG too
	m.dispatchKubePalette("kubernetes-add-cluster")
	if !m.promptOpen || m.promptKind != promptKindKubeAddCluster {
		t.Fatal("no prompt")
	}
	// A bad answer asks again and opens no card.
	m.handleKubePromptSubmit("dev ftp://nope")
	if m.confirmOpen {
		t.Fatal("a bad answer opened the card")
	}
	m.handleKubePromptSubmit("dev https://dev.example:6443")
	if !m.confirmOpen || m.confirmKind != confirmKindKubeAddCluster || m.confirm.SafeID() != "cancel" {
		t.Fatal("no card with Cancel focused")
	}
	if msg := m.confirm.Message(); !strings.Contains(msg, p) || !strings.Contains(msg, "backup") {
		t.Errorf("card message misses the path or the backup note:\n%s", msg)
	}
	view := m.confirm.View()
	for _, want := range []string{"+  name: dev", "server: https://dev.example"} {
		if !strings.Contains(view, want) {
			t.Errorf("card misses %q:\n%s", want, view)
		}
	}
	if b, _ := os.ReadFile(p); string(b) != kcStart {
		t.Fatal("file changed before the OK")
	}

	// Cancel: nothing written, no backup.
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if b, _ := os.ReadFile(p); string(b) != kcStart {
		t.Fatal("cancel changed the file")
	}
	if g, _ := filepath.Glob(p + ".bak-*"); len(g) != 0 {
		t.Fatalf("cancel left a backup: %v", g)
	}

	// OK: backup first, then the new text.
	m.dispatchKubePalette("kubernetes-add-cluster")
	m.handleKubePromptSubmit("dev https://dev.example:6443")
	m.confirmOpen = false
	cmd := m.handleConfirmSelect("add-cluster")
	if cmd == nil {
		t.Fatal("no write command")
	}
	msg := cmd().(kubeAddClusterMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	g, _ := filepath.Glob(p + ".bak-*")
	if len(g) != 1 {
		t.Fatalf("backups = %v", g)
	}
	if b, _ := os.ReadFile(g[0]); string(b) != kcStart {
		t.Error("backup is not the old file")
	}
	b, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(b), strings.TrimSuffix(kcStart, "kind: Config\n")[:20]) || !strings.Contains(string(b), "name: dev") || !strings.Contains(string(b), "current-context: old") {
		t.Errorf("config:\n%s", b)
	}
}

func TestKubeAddClusterRefusesADuplicateBeforeTheCard(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	p := kubeconfigSandbox(t) // after kubeLoaded: it sets KUBECONFIG too
	m.dispatchKubePalette("kubernetes-add-cluster")
	m.handleKubePromptSubmit("old https://x:1")
	if m.confirmOpen {
		t.Fatal("a duplicate opened the card")
	}
	if b, _ := os.ReadFile(p); string(b) != kcStart {
		t.Error("file changed")
	}
}
