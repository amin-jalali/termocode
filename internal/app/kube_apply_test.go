package app

import (
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// kubeApplyBuffer makes path the editor tab (no nvim: the last known one).
func kubeApplyBuffer(m *Model, path string, modified bool) {
	m.bufs = []nvim.BufferInfo{{ID: 7, Path: path, Modified: modified, Filetype: "yaml"}}
	m.activeBuf = 7
}

// CTX-21: a shows the server dry-run diff, then a card with Cancel first;
// only Apply runs kubectl apply, in the scope the card showed.
func TestKubeApplyFileDiffThenCardCancelFirst(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "diff.txt", "--- live\n+++ merged\n-  replicas: 1\n+  replicas: 3\n")
	kubeApplyBuffer(m, "/work/app.yaml", false)
	cmd := kubeKey(t, m, "a")
	if cmd == nil {
		t.Fatal("a ran nothing")
	}
	if m.confirmOpen {
		t.Fatal("a card before the diff")
	}
	msg := cmd().(kubeDiffMsg)
	if msg.err != nil || !msg.diff.Changed {
		t.Fatalf("diff = %+v", msg)
	}
	if f.Count("diff --context dev --namespace shop -f /work/app.yaml") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	m.onKubeDiff(msg)
	if !m.confirmOpen || m.confirmKind != confirmKindKubeApply {
		t.Fatal("no apply card")
	}
	b := m.confirm.Buttons()
	if len(b) != 2 || b[0].ID != "cancel" || b[1].ID != "apply" || m.confirm.SafeID() != "cancel" {
		t.Errorf("buttons %+v safe %q", b, m.confirm.SafeID())
	}
	if f.Count("apply") != 0 {
		t.Fatal("applied before the answer")
	}
	m.confirmOpen = false
	if c := m.handleConfirmSelect("cancel"); c != nil {
		t.Error("cancel returned a command")
	}
	if f.Count("apply") != 0 {
		t.Error("cancel applied")
	}
	m.onKubeDiff(msg)
	m.confirmOpen = false
	c := m.handleConfirmSelect("apply")
	if c == nil {
		t.Fatal("no apply command")
	}
	am := c().(kubeApplyMsg)
	if am.err != nil {
		t.Fatalf("apply = %+v", am)
	}
	if f.Count("apply --context dev --namespace shop -f /work/app.yaml") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	if m.onKubeApply(am) == nil {
		t.Error("no toast / reload after apply")
	}
}

func TestKubeApplyFileNeedsASavedFile(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeApplyBuffer(m, "/work/app.yaml", true)
	if cmd := m.kubeApplyCurrentFile(); cmd != nil {
		if _, ok := cmd().(kubeDiffMsg); ok {
			t.Error("diffed an unsaved buffer")
		}
	}
	kubeApplyBuffer(m, "", false)
	if cmd := m.kubeApplyCurrentFile(); cmd != nil {
		if _, ok := cmd().(kubeDiffMsg); ok {
			t.Error("diffed an unnamed buffer")
		}
	}
	if f.Count("diff") != 0 {
		t.Errorf("calls = %v", f.Calls())
	}
}

func TestKubeApplyNoChangesAsksNothing(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	kubeApplyBuffer(m, "/work/app.yaml", false)
	msg := m.kubeApplyCurrentFile()().(kubeDiffMsg)
	if msg.err != nil || msg.diff.Changed {
		t.Fatalf("diff = %+v", msg)
	}
	m.onKubeDiff(msg)
	if m.confirmOpen {
		t.Error("a card with nothing to apply")
	}
}

func TestKubeApplyPaletteAndPolicy(t *testing.T) {
	found := false
	for _, it := range kubePaletteItems() {
		if it.ID == "kubernetes-apply-file" {
			found = true
		}
	}
	if !found {
		t.Error("no apply palette row")
	}
	if p, ok := aiActionPolicies["kubernetes-apply-file"]; !ok || p.Gate != aiGateNever {
		t.Errorf("policy = %+v", p)
	}
}

// o writes the object's YAML to a private file; a Secret is refused.
func TestKubeEditYAMLWritesAPrivateFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "deployment.yaml", "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: worker\n")
	kubeCursorTo(t, m, "deployments", "worker")
	msg := kubeKey(t, m, "O")().(kubeEditMsg)
	if msg.err != nil {
		t.Fatalf("edit = %+v", msg)
	}
	b, err := os.ReadFile(msg.path)
	if err != nil || !strings.Contains(string(b), "name: worker") {
		t.Fatalf("file %q: %v %q", msg.path, err, b)
	}
	if fi, _ := os.Stat(msg.path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode())
	}
	if cmd := m.kubeEditYAML("secret", "db"); cmd != nil {
		if _, ok := cmd().(kubeEditMsg); ok {
			t.Error("a Secret was written to a file")
		}
	}
}

func TestKubeDiffStat(t *testing.T) {
	add, del := kubeDiffStat("--- a\n+++ b\n@@\n-x\n+y\n+z\n ctx\n")
	if add != 2 || del != 1 {
		t.Errorf("stat = +%d -%d", add, del)
	}
}
