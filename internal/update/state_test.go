package update

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatePath(t *testing.T) {
	got := StatePath(envOf(map[string]string{"XDG_STATE_HOME": "/s"}))
	if got != filepath.Join("/s", "termocode", "update.json") {
		t.Fatalf("got %q", got)
	}
	got = StatePath(envOf(map[string]string{"HOME": "/h"}))
	if got != filepath.Join("/h", ".local", "state", "termocode", "update.json") {
		t.Fatalf("got %q", got)
	}
	if StatePath(envOf(nil)) != "" {
		t.Fatal("no home: want no path")
	}
}

func TestStateSaveLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "termocode", "update.json")
	if s := LoadState(p); !s.LastCheck.IsZero() {
		t.Fatalf("missing file: got %+v", s)
	}
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	want := State{LastCheck: at, Latest: "v0.2.0", NotesURL: "https://x/notes", Skipped: "v0.1.5", Notified: "v0.2.0"}
	if err := SaveState(p, want); err != nil {
		t.Fatal(err)
	}
	got := LoadState(p)
	if !got.LastCheck.Equal(at) || got.Latest != want.Latest || got.Skipped != want.Skipped || got.Notified != want.Notified || got.NotesURL != want.NotesURL {
		t.Fatalf("got %+v", got)
	}
	_ = os.WriteFile(p, []byte("{broken"), 0o600)
	if s := LoadState(p); s.Latest != "" {
		t.Fatal("a broken file reads as empty")
	}
}

func TestDue(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		last time.Time
		want bool
	}{
		{time.Time{}, true},
		{now.Add(-23 * time.Hour), false},
		{now.Add(-24 * time.Hour), true},
		{now.Add(-72 * time.Hour), true},
		{now.Add(5 * time.Hour), true}, // clock moved back: check again
	} {
		if got := Due(State{LastCheck: c.last}, now); got != c.want {
			t.Errorf("Due(last %v) = %v, want %v", c.last, got, c.want)
		}
	}
}

func TestAvailableAndToast(t *testing.T) {
	s := State{Latest: "v0.2.0"}
	if s.Available("v0.1.0") != "v0.2.0" || !s.ShouldToast("v0.1.0") {
		t.Fatal("newer release: want the chip and a toast")
	}
	if s.Available("v0.2.0") != "" || s.Available("dev") != "" {
		t.Fatal("same version or dev build: no chip")
	}
	s.Notified = "v0.2.0"
	if s.Available("v0.1.0") != "v0.2.0" || s.ShouldToast("v0.1.0") {
		t.Fatal("one toast per version; the chip stays")
	}
	s.Skipped = "v0.2.0"
	if s.Available("v0.1.0") != "" {
		t.Fatal("skipped version: no chip")
	}
	s.Latest, s.Notified = "v0.3.0", ""
	if s.Available("v0.1.0") != "v0.3.0" || !s.ShouldToast("v0.1.0") {
		t.Fatal("a newer version than the skipped one shows again")
	}
}
