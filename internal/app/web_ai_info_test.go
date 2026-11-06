package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

func webAIInfo(t *testing.T, r *webFilesRig, kind string) webproto.AIInfo {
	t.Helper()
	var out webproto.AIInfo
	webTestData(t, r.call(webproto.OpAIInfo, &webproto.AIInfoArgs{Kind: kind}), &out)
	if out.Title == "" {
		t.Fatalf("%s: no title", kind)
	}
	return out
}

// webAIInfoText is everything a screen says, as one string.
func webAIInfoText(in webproto.AIInfo) string {
	var b strings.Builder
	b.WriteString(in.Title + "\n" + in.Note + "\n" + in.Text + "\n")
	for _, r := range in.Rows {
		b.WriteString(r.Title + " · " + r.Hint + "\n")
	}
	return b.String()
}

// Spec (web parity: ai-usage, ai-rules, ai-memory, ai-skills, ai-hooks,
// ai-web-tools, ai-mcp-log, mcp-serve-status): each laptop AI screen reads
// on the web as rows or text, the same facts the laptop shows; nothing
// changes on the laptop (no picker, no dialog opens).
func TestWebAIInfoScreens(t *testing.T) {
	r := newWebFilesRig(t)
	root := aiWorkspaceRoot()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Rules\nUse tabs.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(filepath.Join(root, "AGENTS.md")) })
	for _, k := range webproto.AIInfoKinds {
		webAIInfo(t, r, k)
		if r.m.pickerOpen || r.m.confirmOpen || r.m.promptOpen {
			t.Fatalf("%s opened a laptop screen", k)
		}
	}
	if s := webAIInfoText(webAIInfo(t, r, webproto.AIInfoRules)); !strings.Contains(s, "AGENTS.md") {
		t.Errorf("rules:\n%s", s)
	}
	if s := webAIInfoText(webAIInfo(t, r, webproto.AIInfoUsage)); !strings.Contains(strings.ToLower(s), "usage") || strings.Contains(s, "\x1b") {
		t.Errorf("usage (plain text, no escapes):\n%q", s)
	}
	if s := webAIInfoText(webAIInfo(t, r, webproto.AIInfoWebTools)); !strings.Contains(s, "Web fetch") {
		t.Errorf("web tools:\n%s", s)
	}
	if s := webAIInfoText(webAIInfo(t, r, webproto.AIInfoServeStatus)); !strings.Contains(s, "Listening") && !strings.Contains(s, "not") {
		t.Errorf("serve status:\n%s", s)
	}
	cfg := webAIInfo(t, r, webproto.AIInfoServeConfig)
	if !strings.Contains(cfg.Text, `"mcpServers"`) || !strings.Contains(cfg.Text, root) || !cfg.Copy {
		t.Errorf("serve config: %+v", cfg)
	}
}

// Spec (web parity: ai-configure, ai-mcp-log, help-bug-report): the
// provider's setup shows the provider, model and the base URL's host,
// and whether a key is set, never the key, a password or a query; the
// MCP log and the bug report hide registered secrets. The bug report can
// be saved by the browser (nothing is written on the laptop).
func TestWebAIInfoKeepsSecretsOnTheLaptop(t *testing.T) {
	r := newWebFilesRig(t)
	key := "sk-" + "test-info-0123456789abcdef"
	ai.RegisterSecret(key)
	r.m.ai.cfg = ai.Config{Kind: ai.KindOpenAI, Model: "gpt-test", APIKey: key, BaseURL: "https://bob:hunter2pass@llm.example.com/v1?token=qq77zz"}
	s := webAIInfoText(webAIInfo(t, r, webproto.AIInfoConfigure))
	for _, bad := range []string{key, "hunter2pass", "qq77zz", "bob"} {
		if strings.Contains(s, bad) {
			t.Errorf("configure shows %q:\n%s", bad, s)
		}
	}
	if !strings.Contains(s, "llm.example.com") || !strings.Contains(s, "gpt-test") || !strings.Contains(s, "set") {
		t.Errorf("configure:\n%s", s)
	}
	r.m.output.Append(mcpLogChannel, "  github: started with token "+key)
	if s := webAIInfoText(webAIInfo(t, r, webproto.AIInfoMCPLog)); strings.Contains(s, key) || !strings.Contains(s, "github: started") {
		t.Errorf("mcp log:\n%s", s)
	}
	rep := webAIInfo(t, r, webproto.AIInfoBugReport)
	if rep.Save == "" || !strings.HasSuffix(rep.Save, ".md") || strings.Contains(rep.Text, key) {
		t.Errorf("bug report: save=%q", rep.Save)
	}
}
