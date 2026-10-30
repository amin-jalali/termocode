package webproto

import (
	"errors"
	"strings"
	"testing"
)

func TestStrictDecoderRejectsUnknownField(t *testing.T) {
	for _, in := range []string{
		`{"t":"ping","n":"1","extra":true}`,
		`{"t":"hello","v":2,"token":"x","resume":{"conn":"c","topics":{},"more":1}}`,
		`{"t":"bye","code":1000}`,
	} {
		if _, err := DecodeClient([]byte(in)); err == nil {
			t.Errorf("accepted %s", in)
		}
	}
	m, err := DecodeClient([]byte(`{"t":"ping","n":"1","here":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if p := m.(*Ping); p.N != "1" || !p.Here {
		t.Fatalf("got %#v", p)
	}
}

func TestDecodeClientRefusesBadInput(t *testing.T) {
	cases := map[string]string{
		"server kind":   `{"t":"meta","v":2}`,
		"unknown kind":  `{"t":"nope"}`,
		"no kind":       `{"n":"1"}`,
		"not an object": `["t","ping"]`,
		"null":          `null`,
		"trailing":      `{"t":"bye"} {"t":"bye"}`,
		"bad utf-8":     "{\"t\":\"ping\",\"n\":\"\xff\"}",
		"wrong type":    `{"t":"ping","n":1}`,
		"empty":         ``,
	}
	for name, in := range cases {
		if _, err := DecodeClient([]byte(in)); err == nil {
			t.Errorf("%s: accepted %q", name, in)
		}
	}
	big := `{"t":"req","id":1,"op":"x","args":{"s":"` + strings.Repeat("a", MaxFrame) + `"}}`
	if _, err := DecodeClient([]byte(big)); !errors.Is(err, ErrTooBig) {
		t.Fatalf("big frame: err = %v", err)
	}
	if _, err := DecodeClient([]byte(`{"t":"nope"}`)); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("unknown kind: err = %v", err)
	}
}

// Go's decoder keeps the last of two equal keys and matches keys without
// case; the browser and a proxy may see the first one. Both are refused.
func TestStrictDecoderRejectsDuplicateAndFoldedKeys(t *testing.T) {
	for _, in := range []string{
		`{"t":"ping","n":"1","n":"2"}`,
		`{"t":"ping","t":"bye"}`,
		`{"t":"hello","v":2,"token":"a","resume":{"conn":"c","topics":{"git":1,"git":2}}}`,
		`{"t":"hello","v":2,"TOKEN":"x"}`,
		`{"T":"ping","n":"1"}`,
		"{\"t\":\"hello\",\"v\":2,\"toKen\":\"x\"}",
		`{"t":"req","id":1,"op":"x","args":{"a":1,"a":2}}`,
	} {
		if _, err := DecodeClient([]byte(in)); err == nil {
			t.Errorf("accepted %s", in)
		}
	}
	if _, err := DecodeClient([]byte(`{"t":"req","id":1,"op":"x","args":{"a":[{"b":1},{"b":2}]}}`)); err != nil {
		t.Fatalf("equal keys in two array items refused: %v", err)
	}
}
