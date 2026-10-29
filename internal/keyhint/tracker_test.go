package keyhint

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

type fakeTime struct{ now time.Time }

func newTracker(t *testing.T) (*Tracker, *fakeTime) {
	ft := &fakeTime{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	t.Cleanup(clock.Set(func() time.Time { return ft.now }))
	tr := &Tracker{Leader: "<space>", Load: func() []Mapping { return testMaps }}
	return tr, ft
}

// Spec: the leader opens a sequence; the popup shows only after Delay.
func TestPopupShowsAfterDelay(t *testing.T) {
	tr, ft := newTracker(t)
	out := tr.Press("<space>")
	if out.Wait != Delay || !tr.Active() {
		t.Fatalf("leader: %+v active=%v", out, tr.Active())
	}
	ft.now = ft.now.Add(Delay / 2)
	if w := tr.Tick(out.Gen); w <= 0 || w > Delay/2 {
		t.Errorf("early tick wait = %v", w)
	}
	if tr.Shown() {
		t.Fatal("shown before the delay")
	}
	ft.now = ft.now.Add(Delay)
	tr.Tick(out.Gen)
	c, ok := tr.Content()
	if !ok || c.Title != "<space>" || len(c.Entries) != 3 {
		t.Errorf("content = %+v ok=%v", c, ok)
	}
}

// Spec: an expert who types the next key at once never sees the popup;
// the old tick does nothing.
func TestFastTypingNeverShows(t *testing.T) {
	tr, ft := newTracker(t)
	out := tr.Press("<space>")
	tr.Press("f") // a bound leaf: the sequence is done
	ft.now = ft.now.Add(2 * Delay)
	tr.Tick(out.Gen)
	if tr.Shown() || tr.Active() {
		t.Error("popup shown for a fast sequence")
	}
}

// Spec: a key that opens a group goes one level deeper and the popup
// follows at once when it is already visible.
func TestGroupGoesDeeper(t *testing.T) {
	tr, ft := newTracker(t)
	out := tr.Press("<space>")
	ft.now = ft.now.Add(Delay)
	tr.Tick(out.Gen)
	tr.Press("g")
	c, ok := tr.Content()
	if !ok || c.Title != "<space> g" || len(c.Entries) != 2 {
		t.Errorf("level 2 = %+v ok=%v", c, ok)
	}
}

// Spec: Esc closes the popup.
func TestEscCloses(t *testing.T) {
	tr, ft := newTracker(t)
	out := tr.Press("<space>")
	ft.now = ft.now.Add(Delay)
	tr.Tick(out.Gen)
	tr.Press("<esc>")
	if tr.Shown() || tr.Active() {
		t.Error("Esc did not close")
	}
}

// Spec: an unbound key under a visible popup shows "no binding" for a
// moment, then it is gone. Without a popup it stays silent.
func TestUnboundKey(t *testing.T) {
	tr, ft := newTracker(t)
	out := tr.Press("<space>")
	tr.Press("q")
	if tr.Shown() {
		t.Error("silent unbound key showed something")
	}
	out = tr.Press("<space>")
	ft.now = ft.now.Add(Delay)
	tr.Tick(out.Gen)
	out = tr.Press("q")
	c, ok := tr.Content()
	if !ok || c.Notice != "no binding: q" || out.Wait != NoticeFor {
		t.Fatalf("notice = %+v ok=%v out=%+v", c, ok, out)
	}
	ft.now = ft.now.Add(NoticeFor)
	tr.Tick(out.Gen)
	if tr.Shown() {
		t.Error("notice stayed")
	}
}

// Spec: Backspace goes up one level for a 3-key sequence and asks the
// caller to resend the shorter prefix; at level one it just closes.
func TestBackspaceUp(t *testing.T) {
	tr, ft := newTracker(t)
	out := tr.Press("<space>")
	ft.now = ft.now.Add(Delay)
	tr.Tick(out.Gen)
	tr.Press("g")
	up := tr.Press("<bs>")
	if !up.Up || len(up.Prefix) != 1 || up.Prefix[0] != "<space>" {
		t.Fatalf("up = %+v", up)
	}
	if c, _ := tr.Content(); c.Title != "<space>" {
		t.Errorf("title after up = %q", c.Title)
	}
	if again := tr.Press("<bs>"); again.Up || tr.Active() {
		t.Errorf("bs at level one: %+v", again)
	}
}

// Spec: only the leader opens a sequence.
func TestOtherKeysIgnored(t *testing.T) {
	tr, _ := newTracker(t)
	if out := tr.Press("g"); out.Wait != 0 || tr.Active() {
		t.Error("g opened a sequence")
	}
}

// Spec: when the editor stops waiting for the next key (its timeout),
// the popup closes by itself, and a key that arrives in time keeps it.
func TestPopupExpires(t *testing.T) {
	tr, ft := newTracker(t)
	tr.Expire = time.Second
	out := tr.Press("<space>")
	ft.now = ft.now.Add(Delay)
	w := tr.Tick(out.Gen)
	if !tr.Shown() || w <= 0 {
		t.Fatalf("shown=%v wait=%v, want shown and a timer for the expiry", tr.Shown(), w)
	}
	ft.now = ft.now.Add(w / 2)
	tr.Press("g") // a key in time: the clock restarts
	ft.now = ft.now.Add(w)
	if w2 := tr.Tick(out.Gen); w2 <= 0 || !tr.Shown() {
		t.Errorf("closed too early: wait=%v shown=%v", w2, tr.Shown())
	}
	ft.now = ft.now.Add(2 * time.Second)
	tr.Tick(out.Gen)
	if tr.Shown() || tr.Active() {
		t.Error("popup stayed after the editor timeout")
	}
}

// Spec: with a timeout shorter than the delay the editor has already
// let go of the sequence; the popup never shows.
func TestPopupSkippedAfterTimeout(t *testing.T) {
	tr, ft := newTracker(t)
	tr.Expire = Delay / 2
	out := tr.Press("<space>")
	ft.now = ft.now.Add(Delay)
	tr.Tick(out.Gen)
	if tr.Shown() {
		t.Error("popup shown for a dropped sequence")
	}
}
