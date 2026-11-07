package testkit

import (
	"testing"
)

// SCN-17: files that are not plain UTF-8 with LF line ends survive an
// open, edit and save round trip byte for byte (except the typed text).

// encodingFiles are the four odd text files plus nothing else, so no
// language server starts.
func encodingFiles() map[string]string {
	return map[string]string{
		"crlf.txt":   "one\r\ntwo\r\n",
		"bom.txt":    "\xEF\xBB\xBFhello\nworld\n",
		"latin1.txt": "caf\xe9\nna\xefve\n",
		"binary.txt": "head\x00tail\n\x01\x02end\n",
	}
}

// Spec: Given a file with Windows line ends (CRLF), one with a UTF-8 byte
// order mark, one in Latin-1 (not valid UTF-8) and one with NUL and
// control bytes,
// when the user opens each one and presses Ctrl+S without typing,
// then the file on disk is unchanged;
// when the user types "X" at the start and presses Ctrl+S,
// then the file holds "X" plus the old bytes: CRLF stays CRLF, the BOM
// stays, Latin-1 stays Latin-1 and the NUL and control bytes stay.
func TestFileEncodingRoundTrip(t *testing.T) {
	files := encodingFiles()
	d := editorDriver(t, files)
	cases := []struct {
		name string
		lua  string // a buffer fact to check after open
		want string
		line string // the first buffer line as nvim shows it
	}{
		{"crlf.txt", `return vim.bo.fileformat`, "dos", "one"},
		{"bom.txt", `return tostring(vim.bo.bomb)`, "true", "hello"},
		{"latin1.txt", `return vim.bo.fileencoding`, "latin1", "café"},
		{"binary.txt", `return vim.bo.fileformat`, "unix", "head\ntail"},
	}
	for _, c := range cases {
		orig := files[c.name]
		open(d, c.name)
		if got := lua(d, c.lua); got != c.want {
			t.Errorf("%s: %s = %q, want %q", c.name, c.lua, got, c.want)
		}
		// nvim keeps a NUL as "\n" inside a line.
		if got := lua(d, `return vim.api.nvim_buf_get_lines(0, 0, 1, false)[1]`); got != c.line {
			t.Errorf("%s: first line = %q, want %q", c.name, got, c.line)
		}
		d.Key("ctrl+s")
		if got := diskFile(d, c.name); got != orig {
			t.Errorf("%s: save without edit changed the file: %q, want %q", c.name, got, orig)
		}
		d.Type("X")
		if !waitTabs(d, tabsWith(d, c.name+"*")...) {
			got, _ := tabs(d)
			t.Fatalf("%s: tab not marked modified after typing: %v", c.name, got)
		}
		d.Key("ctrl+s")
		want := "X" + orig
		if c.name == "bom.txt" {
			want = "\xEF\xBB\xBFX" + orig[3:]
		}
		if !eventually(d, func() bool { return diskFile(d, c.name) == want }) {
			t.Errorf("%s: after edit and Ctrl+S file = %q, want %q", c.name, diskFile(d, c.name), want)
		}
	}
}

// tabsWith is the current tab list with the active tab replaced by name
// (for example "a.txt*" to wait until the active tab is dirty).
func tabsWith(d *Driver, name string) []string {
	got, act := tabs(d)
	out := make([]string, 0, len(got))
	for _, g := range got {
		if g == act || g == act+"*" {
			g = name
		}
		out = append(out, g)
	}
	return out
}
