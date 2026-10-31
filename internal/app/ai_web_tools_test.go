package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

func aiWebFake(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/doc":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, "<title>API</title><p>Use Foo()</p>")
		case "/brave":
			if r.Header.Get("X-Subscription-Token") != "sk-"+"testwebkey12345" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"web":{"results":[{"title":"Foo docs","url":"https://foo.example","description":"all about Foo"}]}}`)
		}
	}))
	t.Cleanup(srv.Close)
	old, oldEP := aiWebAllowPrivate, aiWebSearchEndpoint
	aiWebAllowPrivate, aiWebSearchEndpoint = true, map[string]string{"brave": srv.URL + "/brave"}
	t.Cleanup(func() { aiWebAllowPrivate, aiWebSearchEndpoint = old, oldEP })
	return srv
}

// Spec (OMX-13): the web tools are off by default; AI: Web Tools… turns
// web fetch on (a read-only tool, no card) and picks a search provider;
// picking one without a key asks for it (masked) and the key goes to
// the secret store; the search sends it only in the provider's header.
func TestAIWebTools(t *testing.T) {
	m, _, root, _ := aiSkillsModel(t)
	srv := aiWebFake(t)
	call := func(context.Context, aiIDEFunc) (string, error) { return "", nil }
	if _, ok := ai.FindTool(m.aiChatTools(root, call), "web_fetch"); ok {
		t.Fatal("web_fetch on by default")
	}
	m.openAIWebTools()
	if m.pickerKind != pickerKindAIWebTools {
		t.Fatal("no picker")
	}
	m.onAIWebToolsPicked(aiWebRowFetch)
	tools := m.aiChatTools(root, call)
	wf, ok := ai.FindTool(tools, "web_fetch")
	if !ok || wf.Mutating {
		t.Fatalf("web_fetch missing or asks: %v", ok)
	}
	out, err := wf.Run(context.Background(), map[string]any{"url": srv.URL + "/doc"})
	if err != nil || !strings.Contains(out, "API") || !strings.Contains(out, "Use Foo()") {
		t.Fatalf("fetch %q %v", out, err)
	}
	if _, ok := ai.FindTool(tools, "web_search"); ok {
		t.Fatal("web_search on without a provider")
	}

	m.onAIWebToolsPicked(aiWebRowSearchPrefix + "brave")
	if !m.promptOpen || m.promptKind != promptKindAIWebKey {
		t.Fatal("no key prompt for a provider without a key")
	}
	m.promptOpen = false
	m.onAIWebKeyEntered("sk-" + "testwebkey12345")
	ws, ok := ai.FindTool(m.aiChatTools(root, call), "web_search")
	if !ok {
		t.Fatal("web_search missing")
	}
	out, err = ws.Run(context.Background(), map[string]any{"query": "foo"})
	if err != nil || !strings.Contains(out, "1. Foo docs") || !strings.Contains(out, "https://foo.example") {
		t.Fatalf("search %q %v", out, err)
	}
	cfg, _ := ai.ConfigPath()
	if b, _ := os.ReadFile(cfg); strings.Contains(string(b), "testwebkey") {
		t.Error("key in config.json")
	}
	m.onAIWebToolsPicked(aiWebRowSearchPrefix + "off")
	m.onAIWebToolsPicked(aiWebRowFetch)
	tools = m.aiChatTools(root, call)
	if _, ok := ai.FindTool(tools, "web_search"); ok {
		t.Error("search still on")
	}
	if _, ok := ai.FindTool(tools, "web_fetch"); ok {
		t.Error("fetch still on")
	}
}

// Spec (OMX-13, OMX-44): a server preset shows its mcp.json entry on a
// card with Cancel first; Cancel writes nothing; Add writes the entry,
// and the browser preset also writes the visual-qa user skill.
func TestAIWebPresets(t *testing.T) {
	m, _, _, userSkills := aiSkillsModel(t)
	m.onAIWebToolsPicked(aiWebRowPresetPrefix + "browser")
	if !m.confirmOpen || m.confirmKind != confirmKindAIWebAdd || m.confirm.SafeID() != "cancel" {
		t.Fatal("no Cancel-first card")
	}
	box := ansi.Strip(m.confirm.Box())
	for _, want := range []string{"@playwright/mcp@latest", "mcp.json", "and the skill"} {
		if !strings.Contains(box, want) {
			t.Errorf("card lacks %q:\n%s", want, box)
		}
	}
	m.answerAIRulesForTest("cancel")
	if len(mcpServerEntries("browser")) != 0 {
		t.Fatal("Cancel wrote the server")
	}
	m.onAIWebToolsPicked(aiWebRowPresetPrefix + "browser")
	m.answerAIRulesForTest("add")
	if len(mcpServerEntries("browser")) != 1 {
		t.Fatal("Add did not write the server")
	}
	if b, err := os.ReadFile(filepath.Join(userSkills, "visual-qa.md")); err != nil || !strings.Contains(string(b), "name: visual-qa") {
		t.Fatalf("skill %v", err)
	}
	if sk, ok := ai.LoadSkills("", userSkills).Find("visual-qa"); !ok || sk.Source != ai.SkillSourceUser {
		t.Error("visual-qa is not a valid user skill")
	}
	m.onAIWebToolsPicked(aiWebRowPresetPrefix + "docs")
	m.answerAIRulesForTest("add")
	list, _ := mcp.LoadAll(mcpUserPath(), "")
	var docs bool
	for _, s := range list {
		docs = docs || (s.Name == "docs" && s.URL == "https://mcp.context7.com/mcp")
	}
	if !docs {
		t.Errorf("docs server %+v", list)
	}
}
