package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/kube"
)

// kubeKindAsk presses : and runs the api-resources call; the kind
// prompt is then open.
func kubeKindAsk(t *testing.T, m *Model) {
	t.Helper()
	cmd := kubeKey(t, m, ":")
	if cmd == nil {
		t.Fatal(": ran nothing")
	}
	msg, ok := cmd().(kubeKindsMsg)
	if !ok {
		t.Fatal("not an api-resources result")
	}
	m.onKubeKinds(msg)
	if !m.promptOpen || m.promptKind != promptKindKubeKind {
		t.Fatalf("no kind prompt (api-resources: %v)", msg.err)
	}
	m.promptOpen = false
}

func TestKubeKindPromptShowsACustomKindSection(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeKindAsk(t, m)
	if f.Count("api-resources --verbs=list --context dev") != 1 {
		t.Fatalf("calls = %v", f.Calls())
	}
	cmd := m.handlePromptSubmit("cert")
	if m.kube.kind == nil || m.kube.kind.Ref() != "certificates.cert-manager.io" {
		t.Fatalf("kind = %+v", m.kube.kind)
	}
	kubeRunLoad(t, m, cmd)
	if f.Count("get certificates.cert-manager.io -o json --context dev --namespace shop") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	found := false
	for _, r := range m.kube.rows() {
		if r.kind == kubeRowSection && r.section == kubeKindSection {
			found = true
		}
	}
	if !found || len(m.kube.kindObjs) == 0 {
		t.Fatalf("no kind section (objs %d)", len(m.kube.kindObjs))
	}
	if got := m.kube.kindTitle(); !strings.Contains(got, "CERTIFICATES") {
		t.Errorf("title = %q", got)
	}
}

func TestKubeKindUnknownAsksAgain(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	kubeKindAsk(t, m)
	if cmd := m.handlePromptSubmit("nosuchkind"); cmd != nil {
		t.Error("an unknown kind loaded")
	}
	if !m.promptOpen || m.prompt.Error() == "" || m.kube.kind != nil {
		t.Errorf("open %v err %q kind %v", m.promptOpen, m.prompt.Error(), m.kube.kind)
	}
}

func TestKubeKindAllNamespaces(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeKindAsk(t, m)
	kubeRunLoad(t, m, m.handlePromptSubmit("certificates -A"))
	if !m.kube.kindAll || f.Count("get certificates.cert-manager.io -o json --context dev --all-namespaces") != 1 {
		t.Fatalf("all %v calls %v", m.kube.kindAll, f.Calls())
	}
	// the four built-in sections stay in the view's namespace
	if f.Count("get pods -o json --context dev --all-namespaces") != 0 {
		t.Error("pods went to all namespaces")
	}
	// A toggles it back
	kubeCursorTo(t, m, kubeKindSection, "web-tls")
	kubeRunLoad(t, m, kubeKey(t, m, "A"))
	if m.kube.kindAll {
		t.Error("A did not toggle")
	}
}

func TestKubeKindEmptyAnswerHidesTheSection(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	kubeKindAsk(t, m)
	kubeRunLoad(t, m, m.handlePromptSubmit("widgets"))
	kubeKindAsk(t, m)
	m.handlePromptSubmit("")
	if m.kube.kind != nil {
		t.Error("the kind stayed")
	}
	for _, r := range m.kube.rows() {
		if r.section == kubeKindSection {
			t.Fatal("the kind section stayed")
		}
	}
}

func TestKubeKindAliasesFromTheConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "termocode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "termocode", "kube_aliases.json"), []byte(`{"tls":"certificates.cert-manager.io"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a := loadKubeAliases()
	if a["tls"] != "certificates.cert-manager.io" {
		t.Fatalf("aliases = %v", a)
	}
	_, m := kubeLoaded(t, 150, 42)
	kubeKindAsk(t, m)
	m.handlePromptSubmit("tls")
	if m.kube.kind == nil || m.kube.kind.Name != "certificates" {
		t.Errorf("kind = %+v", m.kube.kind)
	}
}

func TestKubeKindDescribeUsesTheObjectsNamespace(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeKindAsk(t, m)
	kubeRunLoad(t, m, m.handlePromptSubmit("certificates -A"))
	kubeCursorTo(t, m, kubeKindSection, "api-tls")
	cmd := kubeKey(t, m, "i")
	if cmd == nil {
		t.Fatal("i ran nothing")
	}
	cmd()
	if f.Count("describe certificates.cert-manager.io api-tls --context dev --namespace billing") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
}

func TestKubeKindCompleter(t *testing.T) {
	rs := []kube.APIResource{{Name: "certificates", Group: "cert-manager.io"}, {Name: "certificaterequests", Group: "cert-manager.io"}, {Name: "pods"}}
	c := kubeKindCompleter(rs, nil)
	if got := c("po"); got != "pods" {
		t.Errorf("po -> %q", got)
	}
	if got := c("cert"); got != "certificate" {
		t.Errorf("cert -> %q", got)
	}
	if got := c("zzz"); got != "zzz" {
		t.Errorf("zzz -> %q", got)
	}
}

func TestKubeKindPaletteAIPolicy(t *testing.T) {
	if p := aiActionPolicies["kubernetes-kind"]; p.Gate != aiGateNever {
		t.Errorf("policy = %+v", p)
	}
}
