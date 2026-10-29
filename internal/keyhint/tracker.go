package keyhint

import (
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

const (
	// Delay is how long a sequence waits before the popup shows. A user
	// who types the next key sooner never sees it.
	Delay = 400 * time.Millisecond
	// NoticeFor is how long the "no binding" hint stays.
	NoticeFor = 1200 * time.Millisecond
)

// Tracker watches the keys of a sequence. It never takes a key from the
// editor, except Backspace that goes up a level (Outcome.Up). Time comes
// from clock.Now, so tests move it without sleeping.
type Tracker struct {
	// Leader is the first key that opens a sequence, as a token ("<space>").
	Leader string
	// Load returns the mappings; called once per sequence, on the first
	// key after the leader (or when the popup is due).
	Load func() []Mapping

	seq    []string
	maps   []Mapping
	loaded bool
	last   time.Time
	gen    int
	shown  bool
	notice string
	until  time.Time
}

// Outcome tells the caller what a key did.
type Outcome struct {
	// Wait > 0: schedule Tick(Gen) after Wait.
	Wait time.Duration
	Gen  int
	// Up: Backspace went one level up; the caller must send Prefix to
	// the editor (after cancelling the pending keys) and swallow the key.
	Up     bool
	Prefix []string
}

// Active reports whether a sequence is open (the popup may be hidden).
func (t *Tracker) Active() bool { return len(t.seq) > 0 }

// Shown reports whether the popup (or its notice) is on screen.
func (t *Tracker) Shown() bool { return t.shown || t.notice != "" }

// Reset closes the sequence and the popup. Pending ticks go stale.
func (t *Tracker) Reset() {
	t.seq, t.maps, t.loaded = nil, nil, false
	t.shown, t.notice = false, ""
	t.gen++
}

func (t *Tracker) entries() []Entry {
	if !t.loaded {
		if t.Load != nil {
			t.maps = t.Load()
		}
		t.loaded = true
	}
	return Entries(t.maps, t.seq)
}

func (t *Tracker) arm(wait time.Duration) Outcome {
	t.gen++
	t.last = clock.Now()
	return Outcome{Wait: wait, Gen: t.gen}
}

// Press feeds one key token the user typed into the editor.
func (t *Tracker) Press(tok string) Outcome {
	t.notice = ""
	if len(t.seq) == 0 {
		if t.Leader != "" && tok == t.Leader {
			t.seq = []string{tok}
			t.shown = false
			return t.arm(Delay)
		}
		return Outcome{}
	}
	switch tok {
	case "<esc>":
		t.Reset()
		return Outcome{}
	case "<bs>":
		if len(t.seq) < 2 {
			t.Reset()
			return Outcome{}
		}
		t.seq = t.seq[:len(t.seq)-1]
		out := t.arm(Delay)
		out.Up, out.Prefix = true, append([]string(nil), t.seq...)
		if t.shown {
			out.Wait = 0 // already visible: stay visible one level up
		}
		return out
	}
	for _, e := range t.entries() {
		if e.Key != tok {
			continue
		}
		if !e.Group {
			t.Reset() // the sequence is complete
			return Outcome{}
		}
		t.seq = append(t.seq, tok)
		if t.shown {
			t.last = clock.Now()
			return Outcome{}
		}
		return t.arm(Delay)
	}
	// Not bound under this prefix.
	if t.shown {
		t.seq, t.maps, t.loaded, t.shown = nil, nil, false, false
		t.notice = "no binding: " + strings.TrimSpace(tok)
		t.until = clock.Now().Add(NoticeFor)
		t.gen++
		return Outcome{Wait: NoticeFor, Gen: t.gen}
	}
	t.Reset()
	return Outcome{}
}

// Tick is the delayed check. It shows the popup when the keys have
// rested for Delay, or ends the notice. wait > 0 means "ask me again
// after wait" (the timer fired early).
func (t *Tracker) Tick(gen int) (wait time.Duration) {
	if gen != t.gen {
		return 0
	}
	if t.notice != "" {
		if left := t.until.Sub(clock.Now()); left > 0 {
			return left
		}
		t.notice = ""
		return 0
	}
	if len(t.seq) == 0 || t.shown {
		return 0
	}
	if left := Delay - clock.Since(t.last); left > 0 {
		return left
	}
	if len(t.entries()) == 0 {
		t.Reset() // nothing to show under this prefix
		return 0
	}
	t.shown = true
	return 0
}

// Content is what the popup draws.
type Content struct {
	Title   string // the prefix typed so far, e.g. "<space> g"
	Entries []Entry
	Notice  string // set instead of Entries for the "no binding" hint
}

// Content returns the popup text; ok is false when nothing shows.
func (t *Tracker) Content() (c Content, ok bool) {
	if t.notice != "" {
		return Content{Notice: t.notice}, true
	}
	if !t.shown || len(t.seq) == 0 {
		return Content{}, false
	}
	return Content{Title: strings.Join(t.seq, " "), Entries: t.entries()}, true
}
