package webproto

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestArgsRules(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	bad := []struct{ op, args string }{
		{OpTreeList, `{"path":"/etc"}`},
		{OpTreeList, `{"path":"../x"}`},
		{OpTreeList, `{"path":"a/../../x"}`},
		{OpTreeList, `{"path":"a\u0000b"}`},
		{OpTreeList, `{"path":"` + long(4097) + `"}`},
		{OpGitDiff, `{"path":""}`},
		{OpGitStage, `{"paths":[]}`},
		{OpGitStage, `{"paths":["ok",""]}`},
		{OpGitDiscard, `{"paths":["../out"]}`},
		{OpBufSave, `{"buf":0}`},
		{OpBufSave, `{"buf":-3}`},
		{OpBufSave, `{"buf":4294967296}`},
		{OpTermHistory, `{"buf":1,"before":-1}`},
		{OpBufSetText, `{"buf":1,"tick":-1,"start":{"row":0,"col":0},"end":{"row":0,"col":0},"lines":[]}`},
		{OpBufSetText, `{"buf":1,"tick":1,"start":{"row":-1,"col":0},"end":{"row":0,"col":0},"lines":[]}`},
		{OpBufSetText, `{"buf":1,"tick":1,"start":{"row":2,"col":0},"end":{"row":1,"col":0},"lines":[]}`},
		{OpBufSetText, `{"buf":1,"tick":1,"start":{"row":1,"col":5},"end":{"row":1,"col":4},"lines":[]}`},
		{OpBufSetText, `{"buf":1,"tick":1,"start":{"row":0,"col":0},"end":{"row":0,"col":0},"lines":["a\nb"]}`},
		{OpBufSetText, "{\"buf\":1,\"tick\":1,\"start\":{\"row\":0,\"col\":0},\"end\":{\"row\":0,\"col\":0},\"lines\":[\"a\xffb\"]}"},
		{OpBufSetText, `{"buf":1,"tick":1,"start":{"row":0,"col":0},"end":{"row":0,"col":0},"lines":["a\ud800b"]}`},
		{OpChatSend, `{"text":""}`},
		{OpChatSend, `{"text":"` + long(32*1024+1) + `"}`},
		{OpChatSend, `{"text":"hi","chat_id":"a\u0007"}`},
		{OpChatOpen, `{"id":""}`},
		{OpChatGetMessage, `{"i":-1}`},
		{OpCardsAnswer, `{"id":"c1","choice":""}`},
		{OpCardsAnswer, `{"id":"","choice":"deny"}`},
		{OpTermSend, `{"buf":1,"text":""}`},
		{OpTermSend, `{"buf":1,"text":"` + long(64*1024+1) + `"}`},
		{OpGitShow, `{"hash":"xyz"}`},
		{OpGitShow, `{"hash":"abc"}`},
		{OpGitCommit, `{"message":"  \n "}`},
		{OpGitCommit, `{"message":"` + long(16*1024+1) + `"}`},
		{OpSearchRun, `{"query":""}`},
		{OpSearchRun, `{"query":"` + long(1025) + `"}`},
		{OpSearchRun, `{"query":"a","include":"` + long(513) + `"}`},
		{OpSearchRun, `{"query":"a","max":501}`},
		{OpSearchRun, `{"query":"a","max":-1}`},
		{OpSearchOpen, `{"path":"a.go","line":0}`},
		{OpTasksRun, `{"label":""}`},
		{OpRunsStop, `{"key":""}`},
	}
	for _, c := range bad {
		if _, err := DecodeArgs(c.op, json.RawMessage(c.args)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s %.80s: err = %v, want ErrInvalid", c.op, c.args, err)
		}
	}
	good := []struct{ op, args string }{
		{OpTreeList, `{"path":""}`},
		{OpTreeList, `{"path":"src/a..b/c"}`},
		{OpBufOpen, `{"path":"docs/نوشته.md"}`},
		{OpGitStage, `{"paths":["a.go","b/c.go"]}`},
		{OpBufSetText, `{"buf":3,"tick":41,"start":{"row":1,"col":2},"end":{"row":1,"col":2},"lines":["x := 1",""]}`},
		{OpChatSend, `{"text":"` + long(32*1024) + `"}`},
		{OpTermSend, `{"buf":7,"text":"\u0003"}`},
		{OpGitShow, `{"hash":"27e9ea18"}`},
		{OpGitCommit, `{"message":"fix: one line"}`},
		{OpSearchRun, `{"query":"TODO","regex":true,"max":500}`},
		{OpSearchOpen, `{"path":"a.go","line":1}`},
		{OpCardsAnswer, `{"id":"card-9","choice":"deny"}`},
	}
	for _, c := range good {
		if _, err := DecodeArgs(c.op, json.RawMessage(c.args)); err != nil {
			t.Errorf("%s %.80s: %v", c.op, c.args, err)
		}
	}
}

func TestDecodeArgsStrict(t *testing.T) {
	a, err := DecodeArgs(OpTreeList, json.RawMessage(`{"path":"src"}`))
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := a.(*PathArgs); !ok || p.Path != "src" {
		t.Fatalf("got %#v", a)
	}
	// No args at all is the same as {}.
	if a, err := DecodeArgs(OpChatStop, nil); err != nil {
		t.Fatalf("chat.stop without args: %v", err)
	} else if _, ok := a.(*EmptyArgs); !ok {
		t.Fatalf("got %#v", a)
	}
	if _, err := DecodeArgs("nope.op", json.RawMessage(`{}`)); !errors.Is(err, ErrUnknownOp) {
		t.Fatalf("unknown op: err = %v", err)
	}
	bad := map[string][2]string{
		"unknown field":  {OpTreeList, `{"path":"a","lua":"os.exit()"}`},
		"duplicate key":  {OpTreeList, `{"path":"a","path":"b"}`},
		"folded key":     {OpTreeList, `{"PATH":"a"}`},
		"wrong type":     {OpBufSave, `{"buf":"1"}`},
		"not an object":  {OpBufSave, `[1]`},
		"trailing":       {OpBufSave, `{"buf":1}{}`},
		"empty has args": {OpChatStop, `{"x":1}`},
		"ctl-only field": {OpWebStop, `{"yes":true}`},
	}
	for name, c := range bad {
		if _, err := DecodeArgs(c[0], json.RawMessage(c[1])); err == nil {
			t.Errorf("%s: accepted %s for %s", name, c[1], c[0])
		}
	}
	p, _ := Lookup(OpTreeList)
	big := `{"path":"` + strings.Repeat("a", p.MaxBytes) + `"}`
	if _, err := DecodeArgs(OpTreeList, json.RawMessage(big)); !errors.Is(err, ErrTooBig) {
		t.Fatalf("big args: err = %v", err)
	}
}

// A surrogate pair and an escaped backslash before "ud800" are text the
// user typed: accepted.
func TestArgsGoodUnicodeEscapes(t *testing.T) {
	for _, raw := range []string{
		`{"buf":1,"tick":1,"start":{"row":0,"col":0},"end":{"row":0,"col":0},"lines":["😀"]}`,
		`{"buf":1,"tick":1,"start":{"row":0,"col":0},"end":{"row":0,"col":0},"lines":["a\\ud800"]}`,
		`{"buf":1,"tick":1,"start":{"row":0,"col":0},"end":{"row":0,"col":0},"lines":["ü é"]}`,
	} {
		if _, err := DecodeArgs(OpBufSetText, []byte(raw)); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
}
