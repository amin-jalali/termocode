package help

import (
	"reflect"
	"testing"
)

// The anchors below are what the website (MkDocs, toc permalink: true)
// produces for these headings.
func TestSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Git: stage, commit, sync, conflicts, clone", "git-stage-commit-sync-conflicts-clone"},
		{"Local commands: .termocode/services.json", "local-commands-termocodeservicesjson"},
		{"Categories: quick and deep", "categories-quick-and-deep"},
		{"1. Open a project", "1-open-a-project"},
		{"Icons — icons", "icons-icons"},
		{"Persian, Arabic and other right-to-left text", "persian-arabic-and-other-right-to-left-text"},
		{"2. Find a file with Ctrl+P", "2-find-a-file-with-ctrlp"},
		{"read_file", "read_file"},
		{"Your own configurations (launch.json)", "your-own-configurations-launchjson"},
		{"  Spaces   and -- dashes  ", "spaces-and-dashes"},
		{"Café déjà vu", "cafe-deja-vu"},
		{"ﬁle ligature", "file-ligature"},
		{"Tabs\tand\nlines", "tabs-and-lines"},
		{"فارسی", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := Slug(c.in); got != c.want {
			t.Errorf("Slug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSlugsDuplicates(t *testing.T) {
	got := Slugs([]string{"Keys", "Setup", "Keys", "Keys", "Keys_1", "", "فارسی"})
	want := []string{"keys", "setup", "keys_1", "keys_2", "keys_3", "_1", "_2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Slugs = %q, want %q", got, want)
	}
}

func TestSluggerMatchesSlugs(t *testing.T) {
	in := []string{"A", "A", "B", "A"}
	var s Slugger
	var got []string
	for _, h := range in {
		got = append(got, s.Next(h))
	}
	if want := Slugs(in); !reflect.DeepEqual(got, want) {
		t.Errorf("Slugger = %q, want %q", got, want)
	}
}
