package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/charmbracelet/x/ansi"
)

// The trust card names the project file the entry comes from
// (.mcp.json or .termocode/mcp.json) and its envFile.
func TestMCPTrustCardNamesProjectFile(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct{ path, want string }{
		{filepath.Join(root, ".mcp.json"), "this project's .mcp.json"},
		{mcp.ProjectPath(root), "this project's .termocode/mcp.json"},
	} {
		m := Model{w: 160, h: 40}
		m.mcpTrustDialog(mcp.Server{Name: "gh", Command: "gh-mcp", Source: mcp.SourceProject, SourcePath: tc.path, EnvFile: ".env.mcp"})
		v := ansi.Strip(m.confirm.View())
		if !strings.Contains(v, tc.want) || !strings.Contains(v, "Env file: .env.mcp") {
			t.Errorf("%s: card does not say %q and the env file:\n%s", tc.path, tc.want, v)
		}
	}
}
