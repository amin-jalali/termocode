package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (WEBP-2): the language server installer from the web. The page
// lists the laptop's language servers with their state (viewer); an
// install, reinstall or uninstall waits for a laptop card (controller):
// it runs on the laptop, the card says what it runs.
func TestLSPServerOps(t *testing.T) {
	p, ok := Lookup(OpLSPServers)
	if !ok || p.Role != RoleViewer || p.LaptopCard || p.Audit != AuditNo || p.Phase != PhaseV2 {
		t.Errorf("list row %+v", p)
	}
	p, ok = Lookup(OpLSPServerAsk)
	if !ok || p.Role != RoleController || !p.LaptopCard || p.Audit != AuditYes || p.Phase != PhaseV2 {
		t.Errorf("ask row %+v: a laptop card", p)
	}
	for _, op := range []string{OpLSPServers, OpLSPServerAsk} {
		if _, ok := ResultOf(op); !ok {
			t.Errorf("%s has no result type", op)
		}
	}
	for _, good := range []string{`{"name":"gopls","action":"install"}`, `{"name":"ts_ls","action":"uninstall"}`, `{"name":"lua-language-server","action":"install"}`} {
		if _, err := DecodeArgs(OpLSPServerAsk, json.RawMessage(good)); err != nil {
			t.Errorf("%s: %v", good, err)
		}
	}
	for _, bad := range []string{`{"name":"","action":"install"}`, `{"name":"gopls","action":"run"}`, `{"name":"gopls"}`,
		`{"name":"a b","action":"install"}`, `{"name":"../x","action":"install"}`, `{"name":"` + strings.Repeat("x", 80) + `","action":"install"}`} {
		if _, err := DecodeArgs(OpLSPServerAsk, json.RawMessage(bad)); err == nil {
			t.Errorf("%s: accepted", bad)
		}
	}
}
