package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (web parity: ai-mcp-profile): the web lists the MCP profiles (the
// chat's own marked) as AI: MCP: Switch Profile… does, and switching to
// one, or back to all servers, happens only after the laptop's yes.
func TestWebAIMCPProfileThroughTheLaptop(t *testing.T) {
	r := newWebFilesRig(t)
	m := r.m
	path := mcp.ProjectProfilesPath(aiWorkspaceRoot())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"profiles":{"review":{"description":"read only","servers":["github"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	var list webproto.MCPProfiles
	webTestData(t, r.call(webproto.OpAIMCPProfiles, &webproto.EmptyArgs{}), &list)
	if list.Current != "" || len(list.Profiles) != 1 || list.Profiles[0].Name != "review" || list.Profiles[0].Hint == "" {
		t.Fatalf("list: %+v", list)
	}
	if code := webErrCode(r.call(webproto.OpAIUseMCPProfile, &webproto.AIMCPProfileArgs{Name: "nope"})); code != "not_found" {
		t.Fatalf("unknown: %s", code)
	}
	r.call(webproto.OpAIUseMCPProfile, &webproto.AIMCPProfileArgs{Name: "review"})
	if !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.aiS().mcpProfile != "" {
		t.Fatal("no card, or switched before the yes")
	}
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceConfirm)
	if m.aiS().mcpProfile != "review" {
		t.Fatalf("profile %q", m.aiS().mcpProfile)
	}
	webTestData(t, r.call(webproto.OpAIMCPProfiles, &webproto.EmptyArgs{}), &list)
	if list.Current != "review" {
		t.Fatalf("current %q", list.Current)
	}
	r.call(webproto.OpAIUseMCPProfile, &webproto.AIMCPProfileArgs{All: true})
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceConfirm)
	if m.aiS().mcpProfile != "" {
		t.Fatalf("all: %q", m.aiS().mcpProfile)
	}
}
