//go:build unix

package app

import (
	"testing"
)

// webEditMarks lists the laptop's web-edit marks on buf: "row:text".
func webEditMarks(r *webFilesRig, buf int) []string {
	var out []string
	r.lua(`local b = ...
local ns = vim.api.nvim_create_namespace('termocode_web_edit')
local res = {}
for _, m in ipairs(vim.api.nvim_buf_get_extmarks(b, ns, 0, -1, { details = true })) do
  local d, txt = m[4], ''
  for _, c in ipairs(d.virt_text or {}) do txt = txt .. c[1] end
  res[#res + 1] = m[2] .. ':' .. vim.trim(d.sign_text or '') .. ' ' .. vim.trim(txt)
end
return res`, &out, buf)
	return out
}

// Spec (WEB-F4): where a browser edits, the laptop shows one line
// marker with the device's name; the next edit moves it, so a buffer
// never holds more than one.
func TestWebEditMarksLaptopLine(t *testing.T) {
	r, b := webEditRig(t, "one\ntwo\nthree\n")
	r.c.device = "pixel"
	f := webSetText(r, r.c, b.Buf, b.Tick, 1, 0, 1, 3, "TWO")
	if f["t"] != "res" {
		t.Fatalf("edit: %v", f)
	}
	marks := webEditMarks(r, b.Buf)
	if len(marks) != 1 || marks[0] != "1:▍ ✎ pixel" {
		t.Fatalf("marks after the first edit: %q", marks)
	}
	var res struct{ Tick int }
	webResData(t, f, &res)
	if f := webSetText(r, r.c, b.Buf, res.Tick, 2, 0, 2, 5, "THREE"); f["t"] != "res" {
		t.Fatalf("second edit: %v", f)
	}
	if marks := webEditMarks(r, b.Buf); len(marks) != 1 || marks[0] != "2:▍ ✎ pixel" {
		t.Fatalf("marks after the second edit: %q", marks)
	}
}
