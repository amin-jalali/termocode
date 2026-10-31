package app

import (
	"strings"
	"testing"
)

// MCPX-28: Copy for a teammate puts the entry on the (fake) clipboard as
// an mcpServers block: references stay, literal tokens become
// ${secret:NAME}, no value is copied.
func TestMCPCopyForTeammate(t *testing.T) {
	t.Chdir(t.TempDir())
	m := covMCPModel(t)
	tok := "glpat-" + "test0123456789abcdef"
	writeUserMCP(t, `{"mcpServers":{"gl":{"type":"http","url":"https://gl.example/mcp","headers":{"PRIVATE-TOKEN":"`+tok+`","X-Ref":"${secret:gl_key}"}}}}`)
	m.aiMCP().Reload()
	if !strings.Contains(mcpActionIDList(m, "gl"), "copy-teammate") {
		t.Fatalf("no copy row: %s", mcpActionIDList(m, "gl"))
	}
	var copied []string
	defer SetClipboardSink(func(s string) { copied = append(copied, s) })()
	m.aiS().mcpSel = "gl"
	if m.onMCPServerAction("copy-teammate") == nil {
		t.Error("no toast")
	}
	if len(copied) != 1 {
		t.Fatalf("copied %d times", len(copied))
	}
	c := copied[0]
	if strings.Contains(c, tok) || !strings.Contains(c, `"mcpServers"`) || !strings.Contains(c, "${secret:gl_private_token}") || !strings.Contains(c, "${secret:gl_key}") {
		t.Errorf("copy:\n%s", c)
	}
	if strings.Contains(covToastText(m), tok) {
		t.Error("the toast shows the token")
	}
}
