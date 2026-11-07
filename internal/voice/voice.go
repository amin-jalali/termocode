// Package voice turns speech into text with a program the user chose
// (CMP-48). termocode records nothing itself: the program named in
// voice.json listens on the microphone and prints the words on stdout;
// this package starts it, asks it to stop, and cleans what it printed.
// The caller puts the text into an input box; nothing is sent on its own.
//
// Any local speech-to-text program works: it must print the words (plain
// text, or timestamped lines) and finish on its own after {seconds}, or
// when it gets an interrupt (Ctrl+C), printing what it heard so far.
package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/jsonc"
	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// FileName is the settings file in termocode's config folder.
const FileName = "voice.json"

const (
	DefaultMaxSeconds = 60
	MaxMaxSeconds     = 600
	// MaxTranscript caps the text one dictation adds (runes).
	MaxTranscript = 8000
	// maxOut caps what is read from the program's stdout / stderr.
	maxOut = 256 << 10
	// stopGrace is how long a stopped program may take to print its words.
	stopGrace = 10 * time.Second
	// overrun is how long past {seconds} the program may run before it is
	// stopped for it.
	overrun = 15 * time.Second
)

// Settings is voice.json.
type Settings struct {
	// Command is the program and its arguments, run without a shell.
	// {lang} becomes Language ("auto" when empty), {seconds} the limit.
	Command    []string `json:"command"`
	Language   string   `json:"language,omitempty"`
	MaxSeconds int      `json:"max_seconds,omitempty"`
}

// Load reads dir/voice.json (comments allowed). A missing file is not an
// error: the settings are then not set up.
func Load(dir string) (Settings, error) {
	var s Settings
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(jsonc.Strip(b), &s); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", FileName, err)
	}
	return s, nil
}

// Configured reports whether a program is named.
func (s Settings) Configured() bool {
	return len(s.Command) > 0 && strings.TrimSpace(s.Command[0]) != ""
}

// Seconds is the recording limit.
func (s Settings) Seconds() int {
	switch {
	case s.MaxSeconds <= 0:
		return DefaultMaxSeconds
	case s.MaxSeconds > MaxMaxSeconds:
		return MaxMaxSeconds
	}
	return s.MaxSeconds
}

// Argv is the command with its placeholders filled in.
func (s Settings) Argv() []string {
	lang := strings.TrimSpace(s.Language)
	if lang == "" {
		lang = "auto"
	}
	r := strings.NewReplacer("{lang}", lang, "{seconds}", strconv.Itoa(s.Seconds()))
	out := make([]string, len(s.Command))
	for i, a := range s.Command {
		out[i] = r.Replace(a)
	}
	return out
}

// DefaultBody is the voice.json a first "Open voice.json" writes. Its
// command is empty, so nothing runs until the user names a program.
func DefaultBody() string {
	return `{
  // Voice input (AI: Dictate into Chat…). Name a local speech-to-text
  // program that listens on the microphone and prints the words. It runs
  // without a shell; {seconds} and {lang} are filled in. It should stop
  // by itself after {seconds}, or on Ctrl+C (interrupt), and print what
  // it heard. Example: ["my-dictate", "--seconds", "{seconds}", "--language", "{lang}"]
  "command": [],
  "language": "",
  "max_seconds": 60
}
`
}

var (
	stampRe  = lazyre.New(`\[\d{1,2}:\d{2}(?::\d{2})?(?:[.,]\d+)?\s*-->\s*\d{1,2}:\d{2}(?::\d{2})?(?:[.,]\d+)?\]`)
	markerRe = lazyre.New(`(?i)\[(?:blank_audio|silence|music|noise|inaudible|no speech)\]|\((?:silence|music|noise|inaudible)\)`)
	ansiRe   = lazyre.New(`\x1b\[[0-9;?]*[A-Za-z]`)
)

// Clean turns what the program printed into one paragraph of words:
// timestamps, silence markers, terminal colours and control characters
// go; spaces fold; the text is capped at MaxTranscript runes.
func Clean(out string) string {
	out = ansiRe.ReplaceAllString(out, "")
	out = stampRe.ReplaceAllString(out, " ")
	out = markerRe.ReplaceAllString(out, " ")
	var b strings.Builder
	for _, r := range out {
		if r < 0x20 || r == 0x7f {
			r = ' '
		}
		b.WriteRune(r)
	}
	text := strings.Join(strings.Fields(b.String()), " ")
	if rs := []rune(text); len(rs) > MaxTranscript {
		text = strings.TrimSpace(string(rs[:MaxTranscript]))
	}
	return text
}

// Result is how a dictation ended.
type Result struct {
	Text     string // the cleaned words ("" when canceled)
	Err      error
	Canceled bool // Cancel: the words were thrown away
}

// Session is one running dictation.
type Session struct {
	cmd    *exec.Cmd
	out    *capped
	errOut *capped
	done   chan struct{}
	ctx    context.Context
	res    Result

	mu       sync.Mutex
	canceled bool
	stopped  bool
	cancel   context.CancelFunc
}

// Start runs the program. It stops by itself after Seconds plus a short
// overrun, or when ctx ends.
func Start(ctx context.Context, s Settings) (*Session, error) {
	if !s.Configured() {
		return nil, errors.New("voice input is not set up: name a program in " + FileName)
	}
	argv := s.Argv()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.Seconds())*time.Second+overrun)
	cmd := spawn.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Cancel = func() error { return interrupt(cmd) }
	cmd.WaitDelay = stopGrace
	x := &Session{cmd: cmd, out: &capped{}, errOut: &capped{}, done: make(chan struct{}), ctx: ctx, cancel: cancel}
	cmd.Stdout, cmd.Stderr = x.out, x.errOut
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	crash.Go("voice.Start", func() { x.wait() })
	return x, nil
}

func (x *Session) wait() {
	err := x.cmd.Wait()
	timedOut := errors.Is(x.ctx.Err(), context.DeadlineExceeded)
	x.cancel()
	x.mu.Lock()
	canceled, stopped := x.canceled, x.stopped || timedOut
	x.mu.Unlock()
	switch {
	case canceled:
		x.res = Result{Canceled: true}
	case err != nil && !(stopped && x.out.Len() > 0):
		msg := strings.TrimSpace(Clean(x.errOut.String()))
		if msg == "" {
			msg = err.Error()
		}
		x.res = Result{Err: errors.New(msg)}
	default:
		x.res = Result{Text: Clean(x.out.String())}
	}
	close(x.done)
}

// Stop asks the program to finish and print what it heard: an
// interrupt (ended at once where there is none), and ended for good when
// it is still running stopGrace later.
func (x *Session) Stop() {
	x.mu.Lock()
	x.stopped = true
	x.mu.Unlock()
	x.cancel() // cmd.Cancel interrupts; WaitDelay ends it after stopGrace
}

// Cancel ends the program; its words are thrown away.
func (x *Session) Cancel() {
	x.mu.Lock()
	x.canceled = true
	x.mu.Unlock()
	if x.cmd.Process != nil {
		_ = x.cmd.Process.Kill()
	}
	x.cancel()
}

// Wait blocks until the program ended.
func (x *Session) Wait() Result {
	<-x.done
	return x.res
}

// interrupt sends Ctrl+C to the program; where that does not exist
// (Windows) it is ended.
func interrupt(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}

// capped is a safe buffer that keeps the first maxOut bytes.
type capped struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (c *capped) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := maxOut - c.b.Len(); room > 0 {
		if len(p) > room {
			c.b.Write(p[:room])
		} else {
			c.b.Write(p)
		}
	}
	return len(p), nil
}

func (c *capped) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.String()
}

func (c *capped) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.Len()
}
