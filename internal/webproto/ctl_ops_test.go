package webproto

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The gateway's questions to the laptop (WEB-32/34/48) are control ops:
// never offered to a browser, never in the page's types.
func TestGatewayCtlOps(t *testing.T) {
	for _, op := range []string{OpWebPairAsk, OpWebGrantAsk, OpWebTotpCheck} {
		p, ok := Lookup(op)
		if !ok || p.Role != RoleCtl || p.Audit != AuditYes {
			t.Errorf("%s: row %+v", op, p)
		}
		if Allowed(RoleController, op) || Allowed(RoleViewer, op) {
			t.Errorf("%s offered to a browser", op)
		}
		for _, c := range CapsFor(RoleController) {
			if c == op {
				t.Errorf("%s in caps", op)
			}
		}
	}
}

func TestGatewayCtlArgs(t *testing.T) {
	id := strings.Repeat("a1", 16)
	bad := []struct{ op, args string }{
		{OpWebPairAsk, `{"pair_id":"` + id + `","name":"Phone","code":"12345"}`},
		{OpWebPairAsk, `{"pair_id":"` + id + `","name":"Phone","code":"12345a"}`},
		{OpWebPairAsk, `{"pair_id":"xyz","name":"Phone","code":"123456"}`},
		{OpWebPairAsk, `{"pair_id":"` + id + `","name":"","code":"123456"}`},
		{OpWebPairAsk, `{"pair_id":"` + id + `","name":"a\u0007","code":"123456"}`},
		{OpWebPairAsk, `{"pair_id":"` + id + `","name":"` + strings.Repeat("x", 65) + `","code":"123456"}`},
		{OpWebGrantAsk, `{"device":"","name":"Phone"}`},
		{OpWebGrantAsk, `{"device":"dev-1\n","name":"Phone"}`},
		{OpWebTotpCheck, `{"code":"12 456"}`},
		{OpWebTotpCheck, `{"code":""}`},
	}
	for _, c := range bad {
		if _, err := DecodeArgs(c.op, json.RawMessage(c.args)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s %s: err = %v", c.op, c.args, err)
		}
	}
	good := []struct{ op, args string }{
		{OpWebPairAsk, `{"pair_id":"` + id + `","name":"iPhone · Safari","code":"482913"}`},
		{OpWebPairAsk, `{"pair_id":"` + id + `","name":"Sara's phone","code":"000001","guest":true}`},
		{OpWebGrantAsk, `{"device":"dev-1a2b","name":"iPhone"}`},
		{OpWebTotpCheck, `{"code":"123456","device":"dev-1a2b"}`},
	}
	for _, c := range good {
		if _, err := DecodeArgs(c.op, json.RawMessage(c.args)); err != nil {
			t.Errorf("%s %s: %v", c.op, c.args, err)
		}
	}
}
