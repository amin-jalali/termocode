package webproto

import (
	"errors"
	"strings"
	"testing"
)

var testProjects = []ProjectItem{
	{ID: "aaaaaaaaaaaa", Name: "termocode", Where: "~/termocode", State: "here", WS: "0000000000000001"},
	{ID: "bbbbbbbbbbbb", Name: "shop", Where: "~/src/shop", State: "running", WS: "0000000000000002"},
	{ID: "cccccccccccc", Name: "blog", Where: "~/src/blog", State: "asleep", WS: "0000000000000003"},
	{ID: "dddddddddddd", Name: "api", Where: "~/work/api", State: "asleep", WS: "0000000000000004"},
	{ID: "eeeeeeeeeeee", Name: "API", Where: "~/old/api", State: "running", WS: "0000000000000005"},
}

// Spec (MOB-57): view_show and the Views menu name a project by id, by
// workspace key or by name (any case); none is the home project; a name
// two projects share is refused with both ids; an unknown name lists the
// known ones; a reference with a control character is refused.
func TestProjectRef(t *testing.T) {
	for ref, want := range map[string]string{
		"":                 "aaaaaaaaaaaa",
		"bbbbbbbbbbbb":     "bbbbbbbbbbbb",
		"0000000000000003": "cccccccccccc",
		"Shop":             "bbbbbbbbbbbb",
		"  blog ":          "cccccccccccc",
	} {
		p, err := ProjectRef(testProjects, ref)
		if err != nil || p.ID != want {
			t.Errorf("ProjectRef(%q) = %+v, %v; want %s", ref, p, err, want)
		}
	}
	_, err := ProjectRef(testProjects, "api")
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "dddddddddddd") || !strings.Contains(err.Error(), "eeeeeeeeeeee") {
		t.Errorf("shared name: %v", err)
	}
	_, err = ProjectRef(testProjects, "nope")
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "shop") {
		t.Errorf("unknown name: %v", err)
	}
	for _, bad := range []string{"a\nb", strings.Repeat("x", MaxProjectRef+1)} {
		if _, err := ProjectRef(testProjects, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("ProjectRef(%q) accepted", bad)
		}
	}
	if _, err := ProjectRef(nil, ""); !errors.Is(err, ErrInvalid) {
		t.Error("no home project accepted")
	}
}

// Spec (MOB-57): a pinned view of the home project always runs; another
// project's runs while that project runs, says asleep when it does not,
// and unknown when its key is in no project row.
func TestViewWSState(t *testing.T) {
	home := "0000000000000001"
	for ws, want := range map[string]string{
		"":                 ViewWSLive,
		home:               ViewWSLive,
		"0000000000000002": ViewWSLive,
		"0000000000000003": ViewWSAsleep,
		"00000000000000ff": ViewWSUnknown,
	} {
		if got := ViewWSState(testProjects, home, ws); got != want {
			t.Errorf("ViewWSState(%q) = %s, want %s", ws, got, want)
		}
	}
}

// Spec (MOB-57): a spec's ws is none or a workspace key, never a path or
// a name.
func TestViewWSField(t *testing.T) {
	for _, ok := range []string{"", "0123456789abcdef"} {
		if err := ViewWSField(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"shop", "/home/u/shop", "0123456789ABCDEF", "0123456789abcde"} {
		if err := ViewWSField(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q accepted", bad)
		}
	}
}
