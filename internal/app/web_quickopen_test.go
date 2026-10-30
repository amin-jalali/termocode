package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec: files.list answers the workspace's files, relative and with
// slashes, without the folders the laptop's Go to file skips (.git,
// node_modules) and without folders themselves.
func TestFilesListForQuickOpen(t *testing.T) {
	r := newWebFilesRig(t)
	r.write("cmd/a/main.go", "package main\n")
	r.write("README.md", "# x\n")
	r.write(".git/config", "x\n")
	r.write("node_modules/p/index.js", "x\n")
	var res webproto.FilesListRes
	webResData(t, r.call(webproto.OpFilesList, &webproto.EmptyArgs{}), &res)
	if got := strings.Join(res.Paths, ","); got != "README.md,cmd/a/main.go" || res.Truncated {
		t.Errorf("files.list = %q (truncated %v), want the two files", got, res.Truncated)
	}
}
