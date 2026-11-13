package keyhint

import (
	"strings"
	"testing"
	"time"
)

func TestHasPrefixShorterThanPrefix(t *testing.T) {
	if HasPrefix([]string{"a"}, []string{"a", "b"}) {
		t.Fatal("a shorter key list has the longer prefix")
	}
	if !HasPrefix([]string{"a", "b"}, []string{"a"}) || HasPrefix([]string{"a", "c"}, []string{"a", "b"}) {
		t.Fatal("prefix test")
	}
}

// Keys that differ only in case both show, lower case first.
func TestEntriesLowerCaseFirst(t *testing.T) {
	got := Entries([]Mapping{{LHS: "<Space>A", Desc: "up"}, {LHS: "<Space>a", Desc: "low"}}, []string{"<space>"})
	if len(got) != 2 || got[0].Key != "a" || got[1].Key != "A" {
		t.Fatalf("got %+v", got)
	}
}

func TestKeyLabels(t *testing.T) {
	for in, want := range map[string]string{
		"<cr>": "Enter", "<bs>": "Backspace", "<c-x>": "Ctrl+X", "<a-j>": "Alt+j", "<m-k>": "Alt+k",
		"<space>": "Space", "<esc>": "Esc", "x": "x", "<": "<",
	} {
		if got := KeyLabel(in); got != want {
			t.Errorf("KeyLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// Backspace before the popup shows goes up a level and re-arms the timer;
// the generation grows with every re-arm.
func TestBackspaceBeforePopupArmsTimer(t *testing.T) {
	tr, _ := newTracker(t)
	first := tr.Press("<space>")
	tr.Press("g") // a group: one level down, popup not shown yet
	g := tr.Gen()
	out := tr.Press("<bs>")
	if !out.Up || out.Wait != Delay || out.Gen <= g || len(out.Prefix) != 1 || out.Prefix[0] != "<space>" {
		t.Fatalf("out %+v (first gen %d)", out, first.Gen)
	}
}

// A group key typed before the popup shows re-arms the delay.
func TestGroupBeforePopupArmsTimer(t *testing.T) {
	tr, _ := newTracker(t)
	tr.Press("<space>")
	out := tr.Press("g")
	if out.Wait != Delay || len(tr.seq) != 2 {
		t.Fatalf("out %+v seq %v", out, tr.seq)
	}
}

// The "no binding" notice stays until its time is up; an early tick asks
// to be called again, a late one ends it.
func TestNoticeLifetime(t *testing.T) {
	tr, ft := newTracker(t)
	tr.Press("<space>")
	ft.now = ft.now.Add(Delay)
	tr.Tick(tr.Gen())
	out := tr.Press("q") // not bound
	if !tr.Shown() || out.Wait != NoticeFor {
		t.Fatalf("no notice: %+v", out)
	}
	if c, ok := tr.Content(); !ok || !strings.Contains(c.Notice, "no binding") {
		t.Fatalf("content %+v", c)
	}
	ft.now = ft.now.Add(NoticeFor / 2)
	if w := tr.Tick(out.Gen); w <= 0 {
		t.Fatalf("early tick wait %v", w)
	}
	ft.now = ft.now.Add(NoticeFor)
	if w := tr.Tick(out.Gen); w != 0 || tr.Shown() {
		t.Fatalf("notice not ended: wait %v", w)
	}
}

// A leader with nothing bound under it never shows an empty popup.
func TestEmptyPrefixClosesWithoutPopup(t *testing.T) {
	tr, ft := newTracker(t)
	tr.Load = func() []Mapping { return nil }
	out := tr.Press("<space>")
	ft.now = ft.now.Add(Delay)
	if w := tr.Tick(out.Gen); w != 0 || tr.Active() || tr.Shown() {
		t.Fatalf("empty popup: wait %v active %v", w, tr.Active())
	}
}

// A tick with no open sequence does nothing; Content is empty when the
// popup is not shown.
func TestTickWithoutSequenceAndContentWhenHidden(t *testing.T) {
	tr, _ := newTracker(t)
	if tr.Tick(tr.Gen()) != 0 {
		t.Fatal("tick with no sequence")
	}
	tr.Press("<space>")
	if _, ok := tr.Content(); ok {
		t.Fatal("content before the delay")
	}
}

// The popup closes by itself when the editor stopped waiting (Expire).
func TestPopupExpiresAfterEditorGaveUp(t *testing.T) {
	tr, ft := newTracker(t)
	tr.Expire = 2 * time.Second
	out := tr.Press("<space>")
	ft.now = ft.now.Add(Delay)
	if w := tr.Tick(out.Gen); w <= 0 || !tr.Shown() {
		t.Fatalf("wait %v shown %v", w, tr.Shown())
	}
	ft.now = ft.now.Add(3 * time.Second)
	if w := tr.Tick(out.Gen); w != 0 || tr.Active() {
		t.Fatalf("not expired: wait %v", w)
	}
}

// A narrow screen with a long key column cuts the description to fit; a
// tiny screen gets a short popup or none.
func TestLayoutNarrowCellsAndTooTall(t *testing.T) {
	long := Content{Title: "<space>", Entries: []Entry{{Key: "<c-x>", Desc: strings.Repeat("long ", 20)}}}
	rows, _, _, ok := Layout(long, 20, 10)
	if !ok || len(rows) == 0 {
		t.Fatal("no layout for a narrow screen")
	}
	if rows, _, y, ok := Layout(Content{Title: "t", Entries: manyEntries(30)}, 40, 5); !ok || len(rows) > 4 || y < 0 {
		t.Fatalf("a 5-row screen: %d rows at y=%d ok=%v", len(rows), y, ok)
	}
	if _, _, _, ok := Layout(long, 10, 10); ok {
		t.Fatal("a 10-column screen cannot show the popup")
	}
}
