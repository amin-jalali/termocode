package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEBP-9): file.text reads a workspace file from disk; a secret
// file, a folder and a missing file are refused, binary text is not sent.
func TestWebFileText(t *testing.T) {
	r := newWebFilesRig(t)
	r.write("a.txt", "one\ntwo\n")
	r.write(".env", "TOKEN=x\n")
	r.write("bin.dat", "a\x00b")
	r.write("pkg/b.go", "package pkg\n")
	var res webproto.FileText
	webResData(t, r.call(webproto.OpFileText, &webproto.PathArgs{Path: "a.txt"}), &res)
	if res.Path != "a.txt" || res.Text != "one\ntwo\n" || res.Binary || res.Truncated {
		t.Fatalf("a.txt %+v", res)
	}
	webResData(t, r.call(webproto.OpFileText, &webproto.PathArgs{Path: "bin.dat"}), &res)
	if !res.Binary || res.Text != "" {
		t.Errorf("binary %+v", res)
	}
	if f := r.call(webproto.OpFileText, &webproto.PathArgs{Path: ".env"}); webErrCode(f) != "forbidden" {
		t.Errorf("secret file: %v", f)
	}
	if f := r.call(webproto.OpFileText, &webproto.PathArgs{Path: "pkg"}); f["t"] != "err" {
		t.Errorf("folder: %v", f)
	}
	if f := r.call(webproto.OpFileText, &webproto.PathArgs{Path: "gone.txt"}); webErrCode(f) != "not_found" {
		t.Errorf("missing: %v", f)
	}
}
