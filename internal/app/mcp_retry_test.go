package app

import (
	"context"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// Spec (MCPX-33, UX-19): a failed server's actions start with Retry; it
// starts the server again like Connect.
func TestMCPFailedServerOffersRetryFirst(t *testing.T) {
	m := aiTestModel(t)
	writeUserMCP(t, `{"mcpServers":{"gone":{"command":"termocode-no-such-mcp-server"}}}`)
	_ = m.aiMCP().Connect(context.Background(), nil, "gone")
	if st := serverStatus(&m, "gone"); st.State != mcp.StateFailed {
		t.Fatalf("state %q", st.State)
	}
	items := mcpServerActionItems(&m, "gone")
	if len(items) == 0 || items[0].ID != "retry" || items[0].Title != "Retry" {
		t.Fatalf("first action %+v", items)
	}
	m.aiS().mcpSel = "gone"
	if cmd := m.onMCPServerAction("retry"); cmd == nil {
		t.Fatal("Retry did nothing")
	}
	writeUserMCP(t, fakeServiceEntry(nil))
	m.aiMCP().Reload()
	if items := mcpServerActionItems(&m, "fake"); len(items) > 0 && items[0].ID == "retry" {
		t.Fatal("Retry offered on a server that did not fail")
	}
}
