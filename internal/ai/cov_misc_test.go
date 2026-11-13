package ai

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/blocks"
)

func TestCutMiddleKeepsLinesAndRunesWhole(t *testing.T) {
	// text of short lines: both cuts land on a line break
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "line of text "+strings.Repeat("x", i%7))
	}
	out := cutMiddle(strings.Join(lines, "\n"), 300)
	head, rest, _ := strings.Cut(out, "\n[… ")
	if !strings.HasSuffix(head, "\n") && !strings.HasSuffix(head, "x") {
		t.Errorf("head %q", head[len(head)-10:])
	}
	if !strings.Contains(rest, "characters cut to fit") {
		t.Errorf("no cut note: %q", rest)
	}
	// multi-byte text: no half characters at either end, for 2, 3 and 4 byte runes
	for _, r := range []string{"é", "€", "😀"} {
		for limit := 90; limit < 100; limit++ {
			got := cutMiddle(strings.Repeat(r, 200), limit)
			if !utf8.ValidString(got) {
				t.Fatalf("%q limit %d: broken text", r, limit)
			}
		}
	}
	for s, want := range map[string]bool{"abc": true, "é": true, "\xc3": false, "a\xe2\x82": false, "\xf0\x9f\x98": false, "\x80\x80\x80\x80": false, "€": true} {
		if validCut(s) != want {
			t.Errorf("validCut(%q) != %v", s, want)
		}
	}
}

func TestReasoningHelpers(t *testing.T) {
	if MaxReasoning("low", "high") != "high" || MaxReasoning("high", "low") != "high" || MaxReasoning("", "") != "" {
		t.Error("MaxReasoning")
	}
	if maj, min, ok := claudeVersion("claude-3-5-sonnet"); !ok || maj != 3 {
		t.Errorf("old ids: %d %d %v", maj, min, ok)
	}
	if _, _, ok := claudeVersion("my-proxy-model"); ok {
		t.Error("a proxy name has no version")
	}
	if maj, min, ok := claudeVersion("claude-opus-4-1-20250805"); !ok || maj != 4 || min != 1 {
		t.Errorf("dated id: %d %d", maj, min)
	}
}

func TestRepairToolArgsRefusesWhatItCannotTrust(t *testing.T) {
	for name, s := range map[string]string{
		"fence without newline": "```{\"a\":1}```",
		"fence not closed":      "```json\n{\"a\":1}",
		"quoted not json":       `"{unterminated`,
		"quoted not an object":  `"[1]"`,
		"cut object":            `{"a": "half`,
		"not an object":         `[1,2]`,
		"broken inside":         `{"a" 1}`,
	} {
		if m, ok := RepairToolArgs(s); ok {
			t.Errorf("%s: repaired to %v", name, m)
		}
	}
	m, ok := RepairToolArgs("{\"a\": \"x\r\ty\u0001z\",\n \"b\": [1, 2,\n],}")
	if !ok || m["a"] != "x\r\tyz" {
		t.Errorf("slips: %v %v", m, ok)
	}
	if m, ok := RepairToolArgs("```json\n{\"k\":1}\n```"); !ok || m["k"] != 1.0 {
		t.Errorf("fence: %v %v", m, ok)
	}
	if m, ok := RepairToolArgs(`"{\"k\":2}"`); !ok || m["k"] != 2.0 {
		t.Errorf("string-wrapped: %v %v", m, ok)
	}
}

func TestInlinePolicyEdges(t *testing.T) {
	if ok, why := ShouldAutoTrigger(InlineContext{Before: "x := 1\n", LineBefore: "", LineAfter: "foo", Filetype: "go"}); ok || why == "" {
		t.Errorf("mid-word: %v %q", ok, why)
	}
	if ok, why := ShouldAutoTrigger(InlineContext{Before: "x", LineBefore: "x", LineAfter: " foo(", Filetype: "go"}); ok || why != "code right of the caret" {
		t.Errorf("code right of the caret: %v %q", ok, why)
	}
	if ok, why := ShouldAutoTrigger(InlineContext{Before: "\t", LineBefore: "\t", Filetype: "go", After: "x"}); ok || why != "empty top-level line" && why == "" {
		t.Errorf("indented empty line, no previous: %v %q", ok, why)
	}
	if _, ok := FilterInlineCompletion(InlineContext{}, "  \n", true); ok {
		t.Error("a blank answer is dropped")
	}
	for _, l := range []string{"end", "fi", "done;", "esac", "endif", "endfunction"} {
		if !closesScope(l) {
			t.Errorf("%q closes a scope", l)
		}
	}
	if !inLineComment("go", `x := "a\"b" // note`) || inLineComment("go", `x := "a // not a comment"`) {
		t.Error("comment detection around quotes and escapes")
	}
}

func TestViewHelpers(t *testing.T) {
	if ViewDone(nil) != nil {
		t.Error("nil view")
	}
	good := &blocks.Block{Kind: blocks.KindText, Title: "T", MD: &blocks.MdNode{K: "doc", C: []blocks.MdNode{{K: "p", C: []blocks.MdNode{{K: "text", Text: "x"}}}}}}
	if ViewDone(good) != good {
		t.Error("a valid view is kept")
	}
	if ViewDone(&blocks.Block{Kind: "nope"}) != nil {
		t.Error("an invalid view is dropped")
	}
	if got := ViewText("a\tb\nc\x00d�", 20); got != "a b cd" {
		t.Errorf("ViewText %q", got)
	}
	if got := ViewText("abcdefghij", 5); got != "abcd…" {
		t.Errorf("ViewText cut %q", got)
	}
}

func TestLineLookups(t *testing.T) {
	data := []byte("{\n  \"a\": 1,\n  \"b\": \"a\",\n  \"c\": 3\n}")
	if lineAt(data, -5) != 1 || lineAt(data, 1<<20) != 5 {
		t.Error("lineAt clamps its offset")
	}
	if keyLine(data, "c") != 4 || keyLine(data, "zzz") != 1 {
		t.Errorf("keyLine c=%d zzz=%d", keyLine(data, "c"), keyLine(data, "zzz"))
	}
	// a value that equals the key text is not the key
	if keyLine([]byte("{\"x\": \"a\",\n\"a\": 1}"), "a") != 2 {
		t.Error("keyLine must find the key, not a value")
	}
}
