package dbclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const luaCSVish = `
extension = ".txt"
function header(cols) return table.concat(cols, "|") .. "\n" end
function row(r, i) return i .. ":" .. (r.name or "NULL") .. "\n" end
function footer(n) return "total " .. n .. "\n" end
`

func TestLuaExtractorWritesHeaderRowsAndFooter(t *testing.T) {
	got := exportString(t, ExportFormat("lua:x"), ExportOptions{Lua: luaCSVish}, exportResult())
	want := "id|name|note\n1:O'Hara\n2:back\\slash\n3:=1+1\ntotal 3\n"
	// row 3 has id -3; the script prints only name
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestLuaExtractorSeesNullAsNilAndIndexesAsColumns(t *testing.T) {
	src := `function row(r) return tostring(r[3] == nil) .. tostring(r.note == nil) .. "\n" end`
	got := exportString(t, ExportFormat("lua:x"), ExportOptions{Lua: src}, exportResult())
	if !strings.HasPrefix(got, "truetrue\n") {
		t.Fatalf("got %q", got)
	}
}

func TestLuaExtractorCannotTouchFilesOrRunProcesses(t *testing.T) {
	for _, body := range []string{`io.open("/etc/passwd")`, `os.execute("id")`, `dofile("/etc/passwd")`, `require("os")`, `loadstring("return 1")`} {
		src := "function row(r) " + body + " return '' end"
		var b strings.Builder
		w, err := NewRowWriter(ExportFormat("lua:x"), &b, ExportOptions{Lua: src})
		if err == nil {
			err = WriteResult(w, exportResult())
		}
		if err == nil {
			t.Errorf("%s ran", body)
		}
	}
}

func TestLuaExtractorEndlessLoopIsStopped(t *testing.T) {
	old := luaCallLimit
	luaCallLimit = 50 * 1000 * 1000
	defer func() { luaCallLimit = old }()
	src := `function row(r) while true do end end`
	w, err := NewRowWriter(ExportFormat("lua:x"), &strings.Builder{}, ExportOptions{Lua: src})
	if err == nil {
		err = WriteResult(w, exportResult())
	}
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("err = %v", err)
	}
}

func TestLuaExtractorReportsScriptErrorsWithTheName(t *testing.T) {
	_, err := NewRowWriter(ExportFormat("lua:x"), &strings.Builder{}, ExportOptions{Lua: "function row("})
	if err == nil || !strings.Contains(err.Error(), "Lua") {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewRowWriter(ExportFormat("lua:x"), &strings.Builder{}, ExportOptions{}); err == nil {
		t.Fatal("no script was accepted")
	}
}

func TestFindLuaExtractorsListsScriptsWithTheirExtension(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ndjson.lua"), []byte(`extension = ".ndjson"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := FindLuaExtractors(dir)
	if len(got) != 1 || got[0].Format != "lua:ndjson" || got[0].Ext != ".ndjson" || !strings.Contains(got[0].Title, "ndjson") {
		t.Fatalf("got %+v", got)
	}
	if FindLuaExtractors(filepath.Join(dir, "missing")) != nil {
		t.Fatal("a missing folder listed something")
	}
}
