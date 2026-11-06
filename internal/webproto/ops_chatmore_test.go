package webproto

import (
	"strings"
	"testing"
)

// Spec (web parity, AI chat): the chat history ops. Rename and fork are
// controller ops with an audit line; delete and clear cannot be undone
// and wait for a laptop card; export is a read (a viewer reads the chat
// already). A title is one line of at most MaxChatTitle bytes.
func TestChatHistoryOps(t *testing.T) {
	for op, card := range map[string]bool{OpChatRename: false, OpChatFork: false, OpChatDelete: true, OpChatClear: true} {
		p, ok := Lookup(op)
		if !ok || p.Role != RoleController || p.LaptopCard != card || p.Audit != AuditYes {
			t.Errorf("%s: %+v", op, p)
		}
	}
	if p, ok := Lookup(OpChatExport); !ok || p.Role != RoleViewer || p.LaptopCard {
		t.Errorf("chat.export: %+v", p)
	}
	for op, args := range map[string]string{
		OpChatRename: `{"id":"c-1","title":"Fix the build"}`,
		OpChatFork:   `{"i":3}`,
		OpChatDelete: `{"id":"c-1"}`,
		OpChatClear:  `{}`,
		OpChatExport: `{"id":"c-1"}`,
	} {
		if _, err := DecodeArgs(op, []byte(args)); err != nil {
			t.Errorf("%s %s: %v", op, args, err)
		}
	}
	for _, c := range []struct{ op, args string }{
		{OpChatRename, `{"id":"c-1","title":""}`},
		{OpChatRename, `{"id":"c-1","title":"   "}`},
		{OpChatRename, `{"id":"c-1","title":"a\nb"}`},
		{OpChatRename, `{"id":"","title":"x"}`},
		{OpChatRename, `{"id":"c-1","title":"` + strings.Repeat("t", MaxChatTitle+1) + `"}`},
		{OpChatFork, `{"i":-1}`},
		{OpChatDelete, `{"id":""}`},
		{OpChatExport, `{"id":""}`},
	} {
		if _, err := DecodeArgs(c.op, []byte(c.args)); err == nil {
			t.Errorf("%s %s: accepted", c.op, c.args)
		}
	}
}

// Spec (web parity, AI screens): ai.info reads one of the laptop's AI
// screens (usage, rules, memory, skills, hooks, web tools, the MCP log,
// the MCP server's status and config, the provider's setup, a bug report)
// for a controller; it changes nothing. An unknown kind is refused.
func TestAIInfoOp(t *testing.T) {
	p, ok := Lookup(OpAIInfo)
	if !ok || p.Role != RoleController || p.LaptopCard || p.Audit != AuditYes {
		t.Fatalf("ai.info: %+v", p)
	}
	for _, k := range AIInfoKinds {
		if _, err := DecodeArgs(OpAIInfo, []byte(`{"kind":"`+k+`"}`)); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
	if len(AIInfoKinds) != 11 {
		t.Errorf("kinds %v", AIInfoKinds)
	}
	for _, args := range []string{`{"kind":""}`, `{"kind":"secrets"}`, `{}`} {
		if _, err := DecodeArgs(OpAIInfo, []byte(args)); err == nil {
			t.Errorf("%s accepted", args)
		}
	}
}
