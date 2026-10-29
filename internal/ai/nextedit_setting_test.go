package ai

import (
	"path/filepath"
	"testing"
)

func TestNextEditSettingFollowsInline(t *testing.T) {
	on, off := true, false
	cases := []struct {
		inline, next *bool
		want         bool
	}{
		{nil, nil, true},
		{&off, nil, false}, // default follows ai_inline
		{&on, &off, false},
		{&off, &on, true}, // set on purpose wins
	}
	for i, c := range cases {
		if got := (Settings{Inline: c.inline, NextEdit: c.next}).NextEditEnabled(); got != c.want {
			t.Errorf("case %d: got %v", i, got)
		}
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SaveSettings(path, Settings{NextEdit: &off}); err != nil {
		t.Fatal(err)
	}
	s := LoadSettings(path)
	if s.NextEdit == nil || *s.NextEdit || s.NextEditEnabled() {
		t.Fatalf("saved ai_next_edit not read back: %+v", s.NextEdit)
	}
}
