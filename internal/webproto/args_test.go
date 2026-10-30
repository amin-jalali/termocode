package webproto

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

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
