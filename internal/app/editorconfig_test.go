package app

import "testing"

func TestParseBufferFormat(t *testing.T) {
	s := `{"expandtab":true,"shiftwidth":2,"tabstop":8,"fileencoding":"utf-8","fileformat":"unix",` +
		`"ec_enabled":true,"ec_file":"/r/.editorconfig","ec_props":{"indent_style":"space","indent_size":"2","root":"true"}}`
	bf, ok := parseBufferFormat(s)
	if !ok {
		t.Fatal("parse failed")
	}
	if got := bf.indentLine(); got != "Spaces: 2" {
		t.Errorf("indent = %q", got)
	}
	if got := bf.encodingLine(); got != "UTF-8 (unix)" {
		t.Errorf("encoding = %q", got)
	}
	if got := bf.editorconfigLine(); got != "indent_size=2, indent_style=space  (/r/.editorconfig)" {
		t.Errorf("editorconfig = %q", got)
	}
}

func TestParseBufferFormatEmptyProps(t *testing.T) {
	// vim.json.encode writes an empty Lua table as [].
	bf, ok := parseBufferFormat(`{"expandtab":false,"shiftwidth":0,"tabstop":4,"ec_enabled":true,"ec_file":"","ec_props":[]}`)
	if !ok {
		t.Fatal("parse failed")
	}
	if got := bf.indentLine(); got != "Tabs: 4" {
		t.Errorf("indent = %q", got)
	}
	if got := bf.editorconfigLine(); got != "on, no .editorconfig found" {
		t.Errorf("editorconfig = %q", got)
	}
	bf.ECFile = "/r/.editorconfig"
	if got := bf.editorconfigLine(); got != "no rules match this file  (/r/.editorconfig)" {
		t.Errorf("editorconfig w/ file = %q", got)
	}
	bf.ECEnabled = false
	if got := bf.editorconfigLine(); got != "off (vim.g.editorconfig = false)" {
		t.Errorf("disabled = %q", got)
	}
	if _, ok := parseBufferFormat("nope"); ok {
		t.Error("garbage should fail")
	}
}
