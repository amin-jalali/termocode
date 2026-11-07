package httpclient

import (
	"path/filepath"
	"testing"
	"time"
)

// A pinned entry is saved on disk and survives the history cap.
func TestHistoryPinSurvivesTrim(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "http", "history.jsonl"), Max: 3}
	first, err := s.AppendEntry(Entry{Time: time.Unix(1000, 0), Method: "GET", URL: "http://x/first"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPinned(first.ID, true); err != nil {
		t.Fatal(err)
	}
	for i := range 12 {
		if err := s.Append(Entry{Time: time.Unix(int64(2000+i), 0), Method: "GET", URL: "http://x/n"}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("len = %d, want 3", len(list))
	}
	var got *Entry
	for i := range list {
		if list[i].ID == first.ID {
			got = &list[i]
		}
	}
	if got == nil || !got.Pinned {
		t.Fatalf("pinned entry lost: %+v", list)
	}
	// A fresh store (new process) reads the pin from disk.
	histMu.Lock()
	delete(histFiles, s.Path)
	histMu.Unlock()
	e, ok := s.Get(first.ID)
	if !ok || !e.Pinned {
		t.Fatalf("pin not on disk: %+v %v", e, ok)
	}
	if err := s.SetPinned(first.ID, false); err != nil {
		t.Fatal(err)
	}
	if e, _ := s.Get(first.ID); e.Pinned {
		t.Fatal("unpin did not stick")
	}
	if err := s.SetPinned("nope", true); err == nil {
		t.Fatal("pin of an unknown id must fail")
	}
}

// Only pinned entries over the cap: all of them stay.
func TestTrimListKeepsPinned(t *testing.T) {
	list := []Entry{{ID: "a", Pinned: true}, {ID: "b"}, {ID: "c", Pinned: true}, {ID: "d"}, {ID: "e"}}
	got := trimList(list, 2)
	var ids string
	for _, e := range got {
		ids += e.ID
	}
	if ids != "ac" {
		t.Fatalf("trim(2) = %q, want ac", ids)
	}
	got = trimList(list, 3)
	ids = ""
	for _, e := range got {
		ids += e.ID
	}
	if ids != "ace" {
		t.Fatalf("trim(3) = %q, want ace (oldest unpinned go first)", ids)
	}
}

func TestDayLabel(t *testing.T) {
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.Local)
	cases := []struct {
		t    time.Time
		want string
	}{
		{time.Date(2026, 11, 7, 0, 1, 0, 0, time.Local), "Today"},
		{time.Date(2026, 11, 6, 23, 59, 0, 0, time.Local), "Yesterday"},
		{time.Date(2026, 11, 2, 9, 0, 0, 0, time.Local), "Mon 2 Nov"},
		{time.Date(2025, 12, 31, 9, 0, 0, 0, time.Local), "Wed 31 Dec 2025"},
	}
	for _, c := range cases {
		if got := DayLabel(c.t, now); got != c.want {
			t.Errorf("DayLabel(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}
