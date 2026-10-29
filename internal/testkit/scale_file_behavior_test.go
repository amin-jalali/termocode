package testkit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// bigFile describes one huge file scenario: how it is made and what to
// search for.
type bigFile struct {
	name   string
	make   func() []byte
	needle string // a text near the end of the file
	// Bounds (each ~3× a local run, see bigFiles).
	open, end, search, save time.Duration
	key                     time.Duration // one key on the UI loop
	heapMB                  float64       // Go heap growth
}

func lines100k() []byte {
	var b bytes.Buffer
	for i := 1; i <= 100_000; i++ {
		fmt.Fprintf(&b, "line %06d: the quick brown fox jumps over the lazy dog\n", i)
	}
	b.WriteString("NEEDLE-AT-THE-END\n")
	return b.Bytes()
}

func file50MB() []byte {
	var b bytes.Buffer
	b.Grow(50 << 20)
	row := strings.Repeat("0123456789abcdef", 7) + "\n" // 113 bytes
	for b.Len() < 50<<20 {
		b.WriteString(row)
	}
	b.WriteString("NEEDLE-AT-THE-END\n")
	return b.Bytes()
}

func line1MB() []byte {
	var b bytes.Buffer
	b.WriteString(`{"items":[`)
	for b.Len() < 1<<20 {
		b.WriteString(`{"id":12345,"name":"item","ok":true},`)
	}
	b.WriteString(`"NEEDLE-AT-THE-END"]}` + "\n")
	return b.Bytes()
}

// Local runs (race off): 100k lines: open 0.1 s, end 5 ms, typing the
// search and the jump 1.7 s (17 keys, each settled), save 0.1 s. 50 MB:
// open 0.3 s, end 25 ms, search 3.3 s, save 0.3–0.5 s. 1 MB line: open
// 0.3 s, end 0.16 s, search 1.8 s, save 0.1 s. A key on the UI loop
// takes 1–3 ms; its bound (100 ms) is what still feels instant.
var bigFiles = []bigFile{
	{name: "100k-lines.txt", make: lines100k, needle: "NEEDLE-AT-THE-END",
		open: 2 * time.Second, end: time.Second, search: 5 * time.Second, save: 2 * time.Second,
		key: 100 * time.Millisecond, heapMB: 40},
	{name: "50MB.txt", make: file50MB, needle: "NEEDLE-AT-THE-END",
		open: 4 * time.Second, end: time.Second, search: 10 * time.Second, save: 3 * time.Second,
		key: 100 * time.Millisecond, heapMB: 40},
	{name: "1MB-line.json", make: line1MB, needle: "NEEDLE-AT-THE-END",
		open: 2 * time.Second, end: time.Second, search: 6 * time.Second, save: 2 * time.Second,
		key: 100 * time.Millisecond, heapMB: 40},
}

// Spec (SCN-12): Given a 100k-line file, a 50 MB file and a file that is
// one 1 MB line,
// when the user opens it, goes to the end (Ctrl+End), searches for a
// text at the end (Ctrl+F) and saves after an edit (Ctrl+S),
// then each step finishes within its bound, no key blocks the UI loop
// for long, the Go heap does not keep a copy of the file, and the saved
// file is the original plus the edit.
func TestScaleHugeFiles(t *testing.T) {
	skipShortScale(t)
	for _, bf := range bigFiles {
		t.Run(bf.name, func(t *testing.T) {
			body := bf.make()
			d := New(t, Options{W: 120, H: 34, Nvim: true, Files: map[string]string{"a.txt": "a\n"},
				StepTimeout: 20 * time.Second})
			if err := os.WriteFile(filepath.Join(d.Dir, bf.name), body, 0o644); err != nil {
				t.Fatal(err)
			}
			base := heapMB()

			t0 := time.Now()
			d.Action("open:" + filepath.Join(d.Dir, bf.name))
			if !d.WaitFor(func() bool { _, a := tabs(d); return a == bf.name }, bf.open*raceFactor()) {
				t.Fatalf("%s did not open:\n%s", bf.name, screen(d))
			}
			within(t, "open", time.Since(t0), bf.open)

			t0 = time.Now()
			key := d.Timed(KeyMsg("ctrl+end"))
			last := fmt.Sprint(strings.Count(string(body), "\n"))
			if !d.WaitFor(func() bool { return strings.HasPrefix(cursor(d), last+":") }, bf.end*raceFactor()) {
				t.Fatalf("Ctrl+End: cursor %s, want line %s", cursor(d), last)
			}
			within(t, "go to end", time.Since(t0), bf.end)
			d.Key("ctrl+home")

			// Search: typing into the find bar must not block the UI
			// loop (the search runs in nvim); Enter jumps to the match.
			d.Key("ctrl+f")
			t0 = time.Now()
			for _, r := range bf.needle {
				key = max(key, d.Timed(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}))
				d.Settle(d.opt.MaxSettle)
			}
			d.Key("enter")
			if !d.WaitFor(func() bool { return strings.HasPrefix(cursor(d), last+":") }, bf.search*raceFactor()) {
				t.Fatalf("search: cursor %s, want line %s", cursor(d), last)
			}
			within(t, "type the search and jump", time.Since(t0), bf.search)
			d.Key("esc")

			key = max(key, worstKey(d, "up", "down", "pgup", "pgdown", "right", "left"))
			within(t, "worst key", key, bf.key)

			d.Key("ctrl+end")
			d.Type("Z")
			t0 = time.Now()
			d.Key("ctrl+s")
			want := len(body) + 1
			if !d.WaitFor(func() bool {
				fi, err := os.Stat(filepath.Join(d.Dir, bf.name))
				return err == nil && fi.Size() == int64(want)
			}, bf.save*raceFactor()) {
				fi, _ := os.Stat(filepath.Join(d.Dir, bf.name))
				t.Fatalf("save: file is %v bytes, want %d", fi.Size(), want)
			}
			within(t, "save", time.Since(t0), bf.save)
			got, _ := os.ReadFile(filepath.Join(d.Dir, bf.name))
			if !bytes.Equal(got[:len(body)-1], body[:len(body)-1]) {
				t.Errorf("saved file differs from the original before the edit")
			}
			heapWithin(t, "after open, search and save", base, bf.heapMB)
		})
	}
}
