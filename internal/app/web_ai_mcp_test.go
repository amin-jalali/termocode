package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
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

// Spec (web parity: ai-auto-approve): the web lists what AI: Agent
// Auto-Approve… lists (ask or AUTO per row); a switch runs only after the
// laptop's yes, and only for a row the laptop lists.
func TestWebAIAutoApproveThroughTheLaptop(t *testing.T) {
	r := newWebFilesRig(t)
	m := r.m
	var list webproto.AutoApprove
	webTestData(t, r.call(webproto.OpAIAutoApprove, &webproto.EmptyArgs{}), &list)
	var fw *webproto.AutoApproveRow
	for i := range list.Rows {
		if list.Rows[i].ID == ai.ClassFileWrite {
			fw = &list.Rows[i]
		}
	}
	if fw == nil || fw.On || fw.Title == "" {
		t.Fatalf("rows: %+v", list.Rows)
	}
	if code := webErrCode(r.call(webproto.OpAISetAutoApprove, &webproto.AutoApproveArgs{ID: "everything"})); code != "not_found" {
		t.Fatalf("unknown row: %s", code)
	}
	r.call(webproto.OpAISetAutoApprove, &webproto.AutoApproveArgs{ID: ai.ClassFileWrite})
	if !m.confirmOpen || m.confirmKind != confirmKindWebAsk || slices.Contains(m.aiS().settings.AutoApprove, ai.ClassFileWrite) {
		t.Fatal("no card, or switched before the yes")
	}
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceCancel)
	if slices.Contains(m.aiS().settings.AutoApprove, ai.ClassFileWrite) {
		t.Fatal("cancel switched")
	}
	r.call(webproto.OpAISetAutoApprove, &webproto.AutoApproveArgs{ID: ai.ClassFileWrite})
	m.confirmOpen = false
	m.answerWebAsk(webproto.ChoiceConfirm)
	if !slices.Contains(m.aiS().settings.AutoApprove, ai.ClassFileWrite) {
		t.Fatalf("auto approve %v", m.aiS().settings.AutoApprove)
	}
}
