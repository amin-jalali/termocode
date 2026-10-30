package webproto

import (
	"errors"
	"strconv"
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

func TestDecodeClientChecksLimits(t *testing.T) {
	topics := func(n int) string {
		s := make([]string, n)
		for i := range s {
			s[i] = `"buf:` + strconv.Itoa(i+1) + `"`
		}
		return "[" + strings.Join(s, ",") + "]"
	}
	bad := map[string]string{
		"hello over 4 KiB":   `{"t":"hello","v":2,"token":"` + strings.Repeat("a", 4096) + `"}`,
		"long device name":   `{"t":"hello","v":2,"token":"x","device_name":"` + strings.Repeat("a", 65) + `"}`,
		"resume bad topic":   `{"t":"hello","v":2,"token":"x","resume":{"conn":"c","topics":{"nope":1}}}`,
		"resume 65 topics":   `{"t":"hello","v":2,"token":"x","resume":{"conn":"c","topics":{` + resumeTopics(65) + `}}}`,
		"sub none":           `{"t":"sub","topics":[]}`,
		"sub 33":             `{"t":"sub","topics":` + topics(33) + `}`,
		"sub bad topic":      `{"t":"sub","topics":["nope"]}`,
		"sub twice in one":   `{"t":"sub","topics":["git","git"]}`,
		"unsub bad topic":    `{"t":"unsub","topics":["buf:0"]}`,
		"req id 0":           `{"t":"req","id":0,"op":"tree.list"}`,
		"req no op":          `{"t":"req","id":1,"op":""}`,
		"req long op":        `{"t":"req","id":1,"op":"` + strings.Repeat("a", 65) + `"}`,
		"ping n over 16":     `{"t":"ping","n":"` + strings.Repeat("1", 17) + `"}`,
		"device name ctrl":   `{"t":"hello","v":2,"token":"x","device_name":"a\u0007b"}`,
		"resume conn absent": `{"t":"hello","v":2,"token":"x","resume":{"topics":{}}}`,
	}
	for name, in := range bad {
		if _, err := DecodeClient([]byte(in)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	good := []string{
		`{"t":"sub","topics":` + topics(32) + `}`,
		`{"t":"hello","v":2,"token":"x","device_name":"Phone","resume":{"conn":"c-1","topics":{"git":3,"buf:2":9}}}`,
		`{"t":"req","id":1,"op":"tree.list","args":{"path":""}}`,
		`{"t":"ping","n":"` + strings.Repeat("1", 16) + `"}`,
	}
	for _, in := range good {
		if _, err := DecodeClient([]byte(in)); err != nil {
			t.Errorf("refused %s: %v", in, err)
		}
	}
}

func resumeTopics(n int) string {
	s := make([]string, n)
	for i := range s {
		s[i] = `"buf:` + strconv.Itoa(i+1) + `":1`
	}
	return strings.Join(s, ",")
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
