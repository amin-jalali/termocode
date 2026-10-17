package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// Spec (CX-9): mcp_github_client_id sets termocode's GitHub OAuth App id
// (the device sign-in); empty keeps the build-time value (empty until the
// app is registered); a malformed value is refused.
func TestGitHubClientIDSetting(t *testing.T) {
	m := aiTestModel(t)
	defer mcp.SetGitHubClientID("")
	if got := mcp.GitHubDeviceClientID(); got != mcp.GitHubClientID {
		t.Fatalf("default %q", got)
	}
	m.applySetting("setting-mcp-github-client-id", "Ov23liABCDEF123456")
	if got := mcp.GitHubDeviceClientID(); got != "Ov23liABCDEF123456" {
		t.Errorf("after the setting: %q", got)
	}
	if s := mcp.LoadSettings(mcpConfigPath()); s.GitHubClientID != "Ov23liABCDEF123456" {
		t.Errorf("saved %+v", s)
	}
	m.applySetting("setting-mcp-github-client-id", "bad id; rm")
	if got := mcp.GitHubDeviceClientID(); got != "Ov23liABCDEF123456" {
		t.Errorf("a bad value applied: %q", got)
	}
	m.applySetting("setting-mcp-github-client-id", "")
	if got := mcp.GitHubDeviceClientID(); got != mcp.GitHubClientID {
		t.Errorf("cleared: %q", got)
	}
	if _, ok := aiSettingPolicies["mcp_github_client_id"]; !ok {
		t.Error("no AI policy for the setting")
	}
}
