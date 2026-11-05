// Package agentlog reads the session files that outside CLI agents
// (Claude Code, Codex) write for themselves, as data: turns (the user's
// prompts, the assistant's text, tool calls and their results) and the
// token usage. It is opt-in (the app's agent_transcripts setting) and
// read only: nothing here writes, renames or deletes a file. Every text
// that leaves the package is redacted (ai.Redact) first, because tool
// results hold file contents and may hold secrets.
//
// The formats belong to the CLIs and change without notice, so a line of
// a kind this version does not know is skipped (counted in Skipped), a
// half-written last line waits for its newline, and a file where no line
// is understood is ErrUnknownFormat ("cannot read this session"), never a
// wrong chat.
package agentlog

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// CLI is which agent wrote a file.
type CLI string

const (
	Claude CLI = "claude"
	Codex  CLI = "codex"
)

// Kind is what a turn is.
type Kind int

const (
	User Kind = iota
	Assistant
	ToolCall
	ToolResult
)

func (k Kind) String() string {
	switch k {
	case User:
		return "user"
	case Assistant:
		return "assistant"
	case ToolCall:
		return "tool"
	case ToolResult:
		return "result"
	}
	return "?"
}

// Turn is one entry of the chat.
type Turn struct {
	Kind Kind
	Time time.Time
	// Text is the prompt, the answer or the tool result (redacted; a
	// result is cut to maxResultText).
	Text string
	// Tool is the tool's name (calls and results); Args a one-line
	// summary of the call: the file, the command, "+N -M" of an edit.
	Tool, Args string
	// Failed: the result is an error or a non-zero exit code.
	Failed bool
}

// Usage is the session's token use as the CLI recorded it.
type Usage struct {
	In, Out int // totals
	Context int // the input of the last request (what the context holds)
	Window  int // the model's context window (0: not recorded)
	// LimitPct / LimitReset: the plan limit's used percent and when it
	// resets (Codex records them; zero when not known).
	LimitPct   float64
	LimitReset time.Time
}

// Session is one parsed file.
type Session struct {
	CLI        CLI
	Path       string
	ID         string
	Cwd        string
	Branch     string
	Model      string
	Start, End time.Time
	Turns      []Turn
	Usage      Usage
	// Skipped counts the lines that were not understood.
	Skipped int
	// Truncated: the file was longer than MaxRead; only its newest part
	// was read.
	Truncated bool
}

// Prompts is how many prompts the user typed.
func (s Session) Prompts() int {
	n := 0
	for _, t := range s.Turns {
		if t.Kind == User {
			n++
		}
	}
	return n
}

// FirstPrompt is the first prompt's text ("" for none).
func (s Session) FirstPrompt() string {
	for _, t := range s.Turns {
		if t.Kind == User {
			return t.Text
		}
	}
	return ""
}

// ErrUnknownFormat is a file in which no line was understood.
var ErrUnknownFormat = errors.New("cannot read this session: the file format is not known to this version")

// MaxRead is the most read from one file; a longer file keeps its
// newest part.
const MaxRead = 16 << 20

// maxRead is MaxRead (a var for tests).
var maxRead int64 = MaxRead

// maxResultText is how much of one tool result is kept (it is drawn
// collapsed; the rest stays in the file).
const maxResultText = 4 << 10

// maxTurns is how many turns a session keeps (the newest).
const maxTurns = 5000

// openFile opens a session file (a var: tests count the opens).
var openFile = func(path string) (*os.File, error) { return os.Open(path) }

// redact is ai.Redact (every text out of the package goes through it).
func redact(s string) string { return ai.Redact(s) }

// Parser reads a session file line by line, as a whole or as it grows.
type Parser struct {
	s       Session
	partial []byte
	known   int // lines understood
	lines   int // complete non-blank lines seen
	// calls maps a tool call id to its tool name (for the results).
	calls map[string]string
	// seen marks the assistant message ids already counted in Usage.
	seen map[string]bool
	// dropFirst: the input starts inside a line (a cut window).
	dropFirst bool
}

// NewParser starts an empty session of cli.
func NewParser(cli CLI) *Parser {
	return &Parser{s: Session{CLI: cli}, calls: map[string]string{}, seen: map[string]bool{}}
}

// Write takes the next bytes of the file; complete lines are parsed, a
// half line waits for the rest.
func (p *Parser) Write(b []byte) (int, error) {
	n := len(b)
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			p.partial = append(p.partial, b...)
			break
		}
		line := b[:i]
		if len(p.partial) > 0 {
			line = append(p.partial, line...)
			p.partial = nil
		}
		b = b[i+1:]
		if p.dropFirst {
			p.dropFirst = false
			continue
		}
		p.line(line)
	}
	return n, nil
}

// Session is the session read so far.
func (p *Parser) Session() Session { return p.s }

// Err is ErrUnknownFormat when lines were read and none was understood.
func (p *Parser) Err() error {
	if p.lines > 0 && p.known == 0 {
		return ErrUnknownFormat
	}
	return nil
}

func (p *Parser) line(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	p.lines++
	var ok bool
	switch p.s.CLI {
	case Claude:
		ok = p.claudeLine(line)
	case Codex:
		ok = p.codexLine(line)
	}
	if ok {
		p.known++
	} else {
		p.s.Skipped++
	}
}

// stamp moves Start / End to t.
func (p *Parser) stamp(t time.Time) {
	if t.IsZero() {
		return
	}
	if p.s.Start.IsZero() || t.Before(p.s.Start) {
		p.s.Start = t
	}
	if t.After(p.s.End) {
		p.s.End = t
	}
}

func (p *Parser) add(t Turn) {
	p.s.Turns = append(p.s.Turns, t)
	if len(p.s.Turns) > maxTurns {
		p.s.Turns = append(p.s.Turns[:0:0], p.s.Turns[len(p.s.Turns)-maxTurns:]...)
	}
}

// addText adds a user or assistant turn (redacted; blank is dropped).
func (p *Parser) addText(k Kind, at time.Time, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	p.add(Turn{Kind: k, Time: at, Text: redact(text)})
}

// addResult adds a tool result, cut and redacted.
func (p *Parser) addResult(at time.Time, callID, text string, failed bool) {
	if len(text) > maxResultText {
		text = text[:maxResultText] + "\n…"
	}
	p.add(Turn{Kind: ToolResult, Time: at, Tool: p.calls[callID], Text: redact(text), Failed: failed})
}

// Parse reads a whole session from r. A half last line is left out.
func Parse(cli CLI, r io.Reader) (Session, error) {
	p := NewParser(cli)
	if _, err := io.Copy(p, bufio.NewReaderSize(r, 64<<10)); err != nil {
		return p.Session(), err
	}
	return p.Session(), p.Err()
}

// ReadFile reads the session at path, at most MaxRead bytes (the newest).
func ReadFile(cli CLI, path string) (Session, error) {
	f, err := openFile(path)
	if err != nil {
		return Session{CLI: cli, Path: path}, err
	}
	defer f.Close()
	p := NewParser(cli)
	if st, err := f.Stat(); err == nil && st.Size() > maxRead {
		if _, err := f.Seek(st.Size()-maxRead, io.SeekStart); err == nil {
			p.dropFirst = true
			p.s.Truncated = true
		}
	}
	_, err = io.Copy(p, bufio.NewReaderSize(io.LimitReader(f, maxRead), 64<<10))
	s := p.Session()
	s.Path = path
	if err != nil {
		return s, err
	}
	return s, p.Err()
}

// lineCount counts the lines of a text ("" is 0; a last newline does
// not start a line).
func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
}

// oneLine is the first line of s, cut to n runes.
func oneLine(s string, n int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + " …"
	}
	if r := []rune(s); len(r) > n {
		s = string(r[:n-1]) + "…"
	}
	return s
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
