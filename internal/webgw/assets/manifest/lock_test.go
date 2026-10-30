package manifest

import (
	"strings"
	"testing"
)

func TestLockRoundTrip(t *testing.T) {
	files := map[string][]byte{"b.js": []byte("bb"), "a.css": []byte("a")}
	text := FormatLock(files)
	if !strings.HasPrefix(text, "# Code generated") {
		t.Fatalf("lock has no generated header:\n%s", text)
	}
	if strings.Index(text, "a.css") > strings.Index(text, "b.js") {
		t.Fatalf("lock is not sorted:\n%s", text)
	}
	got, err := ParseLock(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != "a.css" || got[0].Size != 1 || got[0].SRI != SRI([]byte("a")) ||
		got[1].Path != "b.js" || got[1].Size != 2 {
		t.Fatalf("ParseLock = %+v", got)
	}
}

func TestLockRefusesBadLines(t *testing.T) {
	for _, bad := range []string{
		"app.js sha384-x",                  // no size
		"app.js md5-abc 3",                 // not sha384
		"app.js sha384-x -1",               // negative size
		"../app.js sha384-x 1",             // path escapes
		"a.js sha384-x 1\na.js sha384-x 1", // twice
	} {
		if _, err := ParseLock(bad); err == nil {
			t.Errorf("ParseLock(%q) passed", bad)
		}
	}
}
