package httpclient

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
)

// DefaultHistoryMax is the http_history_max default.
const DefaultHistoryMax = 200

// historyBodyMax is how much of a text body one entry keeps.
const historyBodyMax = 256 << 10

// Entry is one line of http/history.jsonl. It holds the request as
// written (templates, redacted headers) and never the resolved URL,
// headers or body, which may contain secrets.
type Entry struct {
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
	By   string    `json:"by"` // "user" or "ai"
	// File + Name point at the request in a .http file (File is
	// workspace-relative when possible); empty for a quick request.
	File string `json:"file,omitempty"`
	Name string `json:"name,omitempty"`
	// The request template.
	Method  string   `json:"method"`
	URL     string   `json:"url"`
	Headers []Header `json:"headers,omitempty"`
	ReqBody *string  `json:"req_body,omitempty"`
	Env     string   `json:"env,omitempty"`
	// The response.
	Status      int      `json:"status,omitempty"`
	StatusText  string   `json:"status_text,omitempty"`
	ElapsedMS   int64    `json:"elapsed_ms"`
	Size        int64    `json:"size"`
	Err         string   `json:"err,omitempty"`
	RespHeaders []Header `json:"resp_headers,omitempty"`
	ContentType string   `json:"content_type,omitempty"`
	Body        string   `json:"body,omitempty"`
	BodyCut     bool     `json:"body_cut,omitempty"`
	Binary      bool     `json:"binary,omitempty"`
	FinalURL    string   `json:"final_url,omitempty"`
	Hops        []Hop    `json:"hops,omitempty"`
}

// NewEntry builds a history entry from the request template and the
// response. Header values are redacted; the URL template has its
// secret query values hidden.
func NewEntry(tmpl Request, file, env, by string, resp *Response) Entry {
	e := Entry{
		Time:    time.Now(),
		By:      by,
		File:    file,
		Name:    tmpl.Name,
		Method:  tmpl.Method,
		URL:     RedactURL(tmpl.URL),
		Headers: RedactTemplateHeaders(tmpl.Headers),
		Env:     env,
	}
	if tmpl.Body != nil {
		b := *tmpl.Body
		e.ReqBody = &b
	}
	if resp != nil {
		e.Status, e.StatusText = resp.Status, resp.StatusText
		e.ElapsedMS = resp.Elapsed.Milliseconds()
		e.Size = resp.Size
		e.Err = resp.Err
		e.RespHeaders = RedactHeaders(resp.Headers)
		e.ContentType = resp.ContentType
		e.Binary = resp.IsBinary
		e.FinalURL = RedactURL(resp.URL)
		for _, h := range resp.Hops {
			e.Hops = append(e.Hops, Hop{Status: h.Status, URL: RedactURL(h.URL)})
		}
		if !resp.IsBinary {
			b := resp.Body
			if len(b) > historyBodyMax {
				b = b[:historyBodyMax]
				e.BodyCut = true
			}
			e.Body = DecodeText(b, resp.Charset)
		}
	}
	return e
}

// Request returns the stored request template.
func (e *Entry) Request() Request {
	r := Request{Method: e.Method, URL: e.URL, Headers: e.Headers, Name: e.Name}
	if e.ReqBody != nil {
		b := *e.ReqBody
		r.Body = &b
	}
	return r
}

// Response rebuilds the stored response (body capped, headers
// redacted) for the page's "open without sending".
func (e *Entry) Response() *Response {
	r := &Response{
		Method: e.Method, URL: e.FinalURL, Status: e.Status, StatusText: e.StatusText,
		Headers: e.RespHeaders, Body: []byte(e.Body), Size: e.Size, Elapsed: time.Duration(e.ElapsedMS) * time.Millisecond,
		ContentType: e.ContentType, IsBinary: e.Binary, Err: e.Err, Hops: e.Hops, Truncated: e.BodyCut, MaxBody: historyBodyMax,
	}
	if r.URL == "" {
		r.URL = e.URL
	}
	r.IsJSON = isJSONType(e.ContentType)
	return r
}

// Line is the picker row: "POST /users · 201 · 84 ms · 2 min ago".
func (e *Entry) Line(now time.Time) string {
	status := fmt.Sprint(e.Status)
	if e.Err != "" {
		status = "ERR"
	}
	label := DeriveName(e.Method, e.URL)
	if e.Name != "" {
		label = e.Method + " " + e.Name
	}
	return fmt.Sprintf("%s · %s · %d ms · %s", label, status, e.ElapsedMS, Ago(now.Sub(e.Time)))
}

// Ago is "just now", "5 s ago", "2 min ago", "3 h ago", "4 d ago".
func Ago(d time.Duration) string {
	switch {
	case d < 5*time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%d s ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d d ago", int(d.Hours()/24))
}

// Store is the history file (JSON lines, oldest first on disk).
type Store struct {
	Path string
	Max  int
	// Redact, when set, runs over every text field before writing (the
	// app passes ai.Redact so registered secrets never reach the disk).
	Redact func(string) string
}

func (s *Store) max() int {
	if s.Max <= 0 {
		return DefaultHistoryMax
	}
	return s.Max
}

func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (s *Store) redact(e *Entry) {
	r := s.Redact
	if r == nil {
		return
	}
	e.URL, e.FinalURL, e.Err, e.Body, e.Name = r(e.URL), r(e.FinalURL), r(e.Err), r(e.Body), r(e.Name)
	if e.ReqBody != nil {
		b := r(*e.ReqBody)
		e.ReqBody = &b
	}
	for i := range e.Headers {
		e.Headers[i].Value = r(e.Headers[i].Value)
	}
	for i := range e.RespHeaders {
		e.RespHeaders[i].Value = r(e.RespHeaders[i].Value)
	}
	for i := range e.Hops {
		e.Hops[i].URL = r(e.Hops[i].URL)
	}
}

// Append adds e (an ID is set when empty). Under the cap it adds one
// line to the file (0600 in a 0700 folder); once the file holds a
// quarter more than Max it is rewritten atomically, trimmed to Max.
func (s *Store) Append(e Entry) error {
	_, err := s.AppendEntry(e)
	return err
}

// AppendEntry is Append that returns the stored entry.
func (s *Store) AppendEntry(e Entry) (Entry, error) {
	if e.ID == "" {
		e.ID = newID()
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	s.redact(&e)
	line, err := encodeEntry(e)
	if err != nil {
		return e, err
	}
	c := histLock(s.Path)
	defer c.mu.Unlock()
	if err := c.sync(s.Path, false); err != nil {
		return e, err
	}
	if c.st == nil || c.lines+1 > s.max()+historySlack(s.max()) {
		if err := c.sync(s.Path, true); err != nil {
			return e, err
		}
		list := append(c.list[:len(c.list):len(c.list)], e)
		return e, c.write(s.Path, trimList(list, s.max()))
	}
	if !c.endsNL {
		line = append([]byte{'\n'}, line...)
	}
	f, err := os.OpenFile(s.Path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return e, err
	}
	_, werr := f.Write(line)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		c.st = nil
		return e, werr
	}
	st, err := os.Stat(s.Path)
	if err != nil {
		c.st = nil
		return e, err
	}
	c.st, c.endsNL = st, true
	c.lines++
	if c.parsed {
		c.list = append(c.list, e)
	}
	return e, nil
}

// List returns the entries, newest first.
func (s *Store) List() ([]Entry, error) {
	c := histLock(s.Path)
	defer c.mu.Unlock()
	if err := c.sync(s.Path, true); err != nil {
		return nil, err
	}
	src := trimList(c.list, s.max())
	list := make([]Entry, len(src))
	for i, e := range src {
		list[len(src)-1-i] = e
	}
	return list, nil
}

// Get returns the entry with id.
func (s *Store) Get(id string) (Entry, bool) {
	list, _ := s.List()
	for _, e := range list {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// Delete removes the entry with id.
func (s *Store) Delete(id string) error {
	c := histLock(s.Path)
	defer c.mu.Unlock()
	if err := c.sync(s.Path, true); err != nil {
		return err
	}
	var out []Entry
	for _, e := range trimList(c.list, s.max()) {
		if e.ID != id {
			out = append(out, e)
		}
	}
	return c.write(s.Path, out)
}

// Clear removes every entry.
func (s *Store) Clear() error {
	c := histLock(s.Path)
	defer c.mu.Unlock()
	return c.write(s.Path, nil)
}

func trimList(list []Entry, n int) []Entry {
	if len(list) > n {
		return list[len(list)-n:]
	}
	return list
}

// historySlack is how far over max the file may grow before a rewrite
// (a quarter, at least 8 lines): the rewrite cost spreads over many
// sends.
func historySlack(n int) int { return max(n/4, 8) }

// histFile is what is known of one history file: its stat when last
// read or written, its line count and (once parsed) its entries. A stat
// that differs (size, time or inode) means someone else wrote the file;
// it is read again.
type histFile struct {
	mu sync.Mutex
	histData
}

type histData struct {
	st     os.FileInfo // nil: file missing or not looked at
	known  bool        // st/lines/endsNL describe the file on disk
	lines  int         // non-empty lines
	endsNL bool
	parsed bool
	list   []Entry // oldest first (when parsed)
}

var (
	histMu    sync.Mutex
	histFiles = map[string]*histFile{}
)

// histLock returns the locked state of path's history file. Every Store
// on the same path shares it, so appends never interleave.
func histLock(path string) *histFile {
	histMu.Lock()
	c := histFiles[path]
	if c == nil {
		c = &histFile{}
		histFiles[path] = c
	}
	histMu.Unlock()
	c.mu.Lock()
	return c
}

// sync makes c describe the file on disk; parse also fills c.list.
func (c *histFile) sync(path string, parse bool) error {
	st, err := os.Stat(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		c.histData = histData{known: true, parsed: true}
		return nil
	}
	if c.known && c.st != nil && os.SameFile(c.st, st) && c.st.Size() == st.Size() && c.st.ModTime().Equal(st.ModTime()) && (c.parsed || !parse) {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	c.st, c.known = st, true
	c.lines = 0
	c.endsNL = len(b) == 0 || b[len(b)-1] == '\n'
	c.parsed, c.list = parse, nil
	var lines [][]byte
	for len(b) > 0 {
		var line []byte
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			line, b = b[:i], b[i+1:]
		} else {
			line, b = b, nil
		}
		if line = bytes.TrimSpace(line); len(line) > 0 {
			lines = append(lines, line)
		}
	}
	c.lines = len(lines)
	if parse {
		c.list = decodeEntries(lines)
	}
	return nil
}

// decodeEntries decodes JSON lines (a bad line is skipped), on several
// cores when there are many: the first history open of a session reads
// the whole file.
func decodeEntries(lines [][]byte) []Entry {
	out := make([]Entry, len(lines))
	ok := make([]bool, len(lines))
	workers := min(runtime.GOMAXPROCS(0), len(lines)/128+1)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		crash.Go("reading HTTP history", func() {
			defer wg.Done()
			for i := w; i < len(lines); i += workers {
				ok[i] = json.Unmarshal(lines[i], &out[i]) == nil
			}
		})
	}
	wg.Wait()
	n := 0
	for i := range out {
		if ok[i] {
			out[n] = out[i]
			n++
		}
	}
	clear(out[n:])
	return out[:n]
}

// write rewrites the file with list (atomic, 0600) and remembers it.
func (c *histFile) write(path string, list []Entry) error {
	var buf bytes.Buffer
	for _, e := range list {
		line, err := encodeEntry(e)
		if err != nil {
			return err
		}
		buf.Write(line)
	}
	c.known, c.st = false, nil
	if err := writePrivate(path, buf.Bytes()); err != nil {
		return err
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	c.histData = histData{st: st, known: true, lines: len(list), endsNL: true, parsed: true, list: list}
	return nil
}

// encodeEntry is one JSON line (no HTML escaping, ending in \n).
func encodeEntry(e Entry) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(e); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
