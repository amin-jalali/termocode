package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (CX-3): the account comes from structured content, a nested
// "user", a JSON text or the first e-mail in plain text; nothing usable
// gives "".
func TestAccountFrom(t *testing.T) {
	text := func(s string) CallResult { return CallResult{Content: []Content{{Type: "text", Text: s}}} }
	cases := []struct {
		r    CallResult
		want string
	}{
		{CallResult{StructuredContent: json.RawMessage(`{"user":{"login":"amin-jalali"}}`)}, "amin-jalali"},
		{text(`{"username":"perkino"}`), "perkino"},
		{text(`{"id": 7}`), ""},
		{text("You are signed in as (amin@example.com)."), "amin@example.com"},
		{text("no account here"), ""},
	}
	for _, c := range cases {
		if got := accountFrom(c.r); got != c.want {
			t.Errorf("accountFrom(%s %q) = %q, want %q", c.r.StructuredContent, c.r.Text(), got, c.want)
		}
	}
}

// Spec (CX-5): mcp.json edits refuse bad names, missing entries and a
// damaged file, which is never overwritten.
func TestEditServersRefusals(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mcp.json")
	if err := AddServer(p, "bad name", map[string]any{"command": "x"}); err == nil || !strings.Contains(err.Error(), "letters, digits") {
		t.Fatalf("AddServer: %v", err)
	}
	if err := PutServer(p, "bad/name", map[string]any{"command": "x"}); err == nil || !strings.Contains(err.Error(), "letters, digits") {
		t.Fatalf("PutServer: %v", err)
	}
	if err := SetDisabled(p, "nope", true); err == nil || !strings.Contains(err.Error(), "no server named nope") {
		t.Fatalf("SetDisabled: %v", err)
	}
	if err := os.WriteFile(p, []byte("{damaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddServer(p, "ok", map[string]any{"command": "x"}); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("damaged: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "{damaged" {
		t.Fatal("the damaged file was overwritten")
	}
	if err := RemoveServer(dir, "x"); err == nil {
		t.Fatal("a folder as mcp.json was accepted")
	}
	file := filepath.Join(dir, "afile")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddServer(filepath.Join(file, "mcp.json"), "ok", map[string]any{"command": "x"}); err == nil {
		t.Fatal("mcp.json under a file was accepted")
	}
}

// Spec (MC-2): the confirm line shows arguments short: sorted, cut at 40
// characters, quoted when empty or with spaces, non-strings printed.
func TestShortArgs(t *testing.T) {
	got := shortArgs(map[string]any{"b": strings.Repeat("x", 50), "a": "", "c": "two words", "d": 3, "e": []string{"p"}})
	want := `a="" b=` + strings.Repeat("x", 39) + `… c="two words" d=3 e=[p]`
	if got != want {
		t.Fatalf("shortArgs = %s\nwant       %s", got, want)
	}
	if r := schemaRequired(map[string]any{"required": []string{"a", "b"}}); len(r) != 2 {
		t.Fatalf("schemaRequired []string = %v", r)
	}
}
