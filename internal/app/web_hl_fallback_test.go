package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec: when nvim has no parser for the file (an install with no Go
// parser), buf.hl answers the hub's own colours, not "none".
func TestHlFallsBackWhenNvimHasNoParser(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	old := webHlNoParser
	webHlNoParser = true // as an nvim with no parsers at all
	t.Cleanup(func() { webHlNoParser = old })
	r.write("a.go", "package main\n\nfunc main() {}\n")
	b := webBufOpen(t, r, r.c, "a.go")
	var res webproto.HlRes
	webResData(t, webHl(r, b.Buf, 0, 99), &res)
	if res.None || res.Tick != b.Tick || res.Last != 3 {
		t.Fatalf("head %+v", res)
	}
	if g := webHlGroup(res, 0, 0); g != "keyword.import" && g != "keyword" {
		t.Errorf("`package` is %q (%v %v)", g, res.Spans, res.Groups)
	}
	r.write("n.txt", "just words\n")
	b = webBufOpen(t, r, r.c, "n.txt")
	webResData(t, webHl(r, b.Buf, 0, 9), &res)
	if !res.None || len(res.Spans) != 0 {
		t.Errorf("plain text: %+v", res)
	}
}
