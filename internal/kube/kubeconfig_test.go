package kube

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

const kubectlStyle = `apiVersion: v1
# my clusters
clusters:
- cluster:
    server: https://old.example:6443
  name: old
contexts:
- context:
    cluster: old
    user: me
  name: old
current-context: old
kind: Config
users:
- name: me
  user:
    token: abc
`

func parseCfg(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatalf("result is not YAML: %v\n%s", err, b)
	}
	return m
}

func names(m map[string]any, key string) []string {
	var out []string
	l, _ := m[key].([]any)
	for _, e := range l {
		if em, ok := e.(map[string]any); ok {
			if n, ok := em["name"].(string); ok {
				out = append(out, n)
			}
		}
	}
	return out
}

func TestPlanAddClusterOnlyAddsLines(t *testing.T) {
	out, err := PlanAddCluster([]byte(kubectlStyle), ClusterAdd{Name: "dev", Server: "https://dev.example:6443", CAFile: "/etc/ca.pem"})
	if err != nil {
		t.Fatal(err)
	}
	// Every old line is still there, in order; the comment too.
	oldLines := strings.Split(kubectlStyle, "\n")
	newLines := strings.Split(string(out), "\n")
	j := 0
	for _, l := range newLines {
		if j < len(oldLines) && l == oldLines[j] {
			j++
		}
	}
	if j != len(oldLines) {
		t.Fatalf("old text changed:\n%s", out)
	}
	m := parseCfg(t, out)
	if got := strings.Join(names(m, "clusters"), ","); got != "old,dev" {
		t.Errorf("clusters = %s", got)
	}
	if got := strings.Join(names(m, "contexts"), ","); got != "old,dev" {
		t.Errorf("contexts = %s", got)
	}
	if m["current-context"] != "old" {
		t.Errorf("current-context changed: %v", m["current-context"])
	}
	c := m["clusters"].([]any)[1].(map[string]any)["cluster"].(map[string]any)
	if c["server"] != "https://dev.example:6443" || c["certificate-authority"] != "/etc/ca.pem" {
		t.Errorf("cluster = %v", c)
	}
}

func TestPlanAddClusterIndentedStyle(t *testing.T) {
	in := "apiVersion: v1\nkind: Config\nclusters:\n  - cluster:\n      server: https://a:1\n    name: a\ncontexts:\n  - context:\n      cluster: a\n    name: a\n"
	out, err := PlanAddCluster([]byte(in), ClusterAdd{Name: "b", Server: "https://b:2"})
	if err != nil {
		t.Fatal(err)
	}
	m := parseCfg(t, out)
	if strings.Join(names(m, "clusters"), ",") != "a,b" || strings.Join(names(m, "contexts"), ",") != "a,b" {
		t.Errorf("result:\n%s", out)
	}
}

func TestPlanAddClusterMissingKeysAndEmptyFile(t *testing.T) {
	for _, in := range []string{"", "apiVersion: v1\nkind: Config\n", "kind: Config\nusers: []\n"} {
		out, err := PlanAddCluster([]byte(in), ClusterAdd{Name: "n", Server: "https://n:1"})
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		m := parseCfg(t, out)
		if strings.Join(names(m, "clusters"), ",") != "n" || strings.Join(names(m, "contexts"), ",") != "n" {
			t.Errorf("%q gave:\n%s", in, out)
		}
		if m["kind"] != "Config" || m["apiVersion"] != "v1" {
			t.Errorf("%q: kind/apiVersion missing:\n%s", in, out)
		}
	}
}

func TestPlanAddClusterRefusals(t *testing.T) {
	if _, err := PlanAddCluster([]byte(kubectlStyle), ClusterAdd{Name: "old", Server: "https://x:1"}); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("duplicate cluster: %v", err)
	}
	flow := "apiVersion: v1\nkind: Config\nclusters: []\ncontexts: []\n"
	if _, err := PlanAddCluster([]byte(flow), ClusterAdd{Name: "n", Server: "https://n:1"}); err == nil {
		t.Error("unusual layout must be refused")
	}
	if _, err := PlanAddCluster([]byte("{{{ not yaml"), ClusterAdd{Name: "n", Server: "https://n:1"}); err == nil {
		t.Error("broken file must be refused")
	}
}

func TestCheckClusterAdd(t *testing.T) {
	good := ClusterAdd{Name: "dev-1.x", Server: "https://10.0.0.1:6443"}
	if err := CheckClusterAdd(good); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []ClusterAdd{
		{Name: "", Server: "https://a:1"},
		{Name: "-flag", Server: "https://a:1"},
		{Name: "a b", Server: "https://a:1"},
		{Name: "a", Server: "ftp://a"},
		{Name: "a", Server: "https://a b"},
		{Name: "a", Server: "https://a:1", CAFile: "rel/ca.pem"},
		{Name: "a", Server: "https://a:1", CAFile: "/x\ny"},
	} {
		if CheckClusterAdd(bad) == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestPlanAddClusterQuotesNamesThatLookLikeOtherTypes(t *testing.T) {
	out, err := PlanAddCluster([]byte(kubectlStyle), ClusterAdd{Name: "123", Server: "https://a:1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(parseCfg(t, out), "clusters"), ","); got != "old,123" {
		t.Errorf("clusters = %s\n%s", got, out)
	}
}

func TestDiffTextShowsOnlyChanges(t *testing.T) {
	out, _ := PlanAddCluster([]byte(kubectlStyle), ClusterAdd{Name: "dev", Server: "https://dev:1"})
	d := DiffText(kubectlStyle, string(out))
	if !strings.Contains(d, "+  name: dev") || !strings.Contains(d, "+    server: https://dev:1") {
		t.Errorf("diff misses added lines:\n%s", d)
	}
	for _, l := range strings.Split(d, "\n") {
		if strings.HasPrefix(l, "-") {
			t.Errorf("removed line in a pure add: %q", l)
		}
	}
	if DiffText("a\n", "a\n") != "" {
		t.Error("same text must give an empty diff")
	}
}

func TestKubeconfigPath(t *testing.T) {
	home := "/h"
	env := func(v string) string {
		if v == "KUBECONFIG" {
			return ""
		}
		return ""
	}
	if got := KubeconfigPath(env, home); got != filepath.Join(home, ".kube", "config") {
		t.Errorf("default = %s", got)
	}
	env2 := func(string) string { return "/a/one" + string(os.PathListSeparator) + "/b/two" }
	if got := KubeconfigPath(env2, home); got != "/a/one" {
		t.Errorf("first entry = %s", got)
	}
}

func TestWriteKubeconfigBacksUpFirst(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config")
	if err := os.WriteFile(p, []byte(kubectlStyle), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 14, 9, 8, 7, 0, time.UTC)
	bak, err := WriteKubeconfig(p, []byte("new: 1\n"), now)
	if err != nil {
		t.Fatal(err)
	}
	if want := p + ".bak-20261114-090807"; bak != want {
		t.Errorf("backup = %s, want %s", bak, want)
	}
	if b, _ := os.ReadFile(bak); string(b) != kubectlStyle {
		t.Errorf("backup differs: %q", b)
	}
	if b, _ := os.ReadFile(p); string(b) != "new: 1\n" {
		t.Errorf("config = %q", b)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", st.Mode())
	}
}

func TestWriteKubeconfigNewFileNeedsNoBackup(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".kube", "config")
	bak, err := WriteKubeconfig(p, []byte("a: 1\n"), time.Now())
	if err != nil || bak != "" {
		t.Fatalf("bak=%q err=%v", bak, err)
	}
	if b, _ := os.ReadFile(p); string(b) != "a: 1\n" {
		t.Errorf("config = %q", b)
	}
}
