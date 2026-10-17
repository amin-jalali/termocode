package httpclient

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
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
	mu     sync.Mutex
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

// Append adds e (an ID is set when empty), trims to Max and rewrites
// the file atomically (0600 in a 0700 folder). It returns the stored
// entry's id through e.ID's value on disk.
func (s *Store) Append(e Entry) error {
	_, err := s.AppendEntry(e)
	return err
}

// AppendEntry is Append that returns the stored entry.
func (s *Store) AppendEntry(e Entry) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.ID == "" {
		e.ID = newID()
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	s.redact(&e)
	list, err := s.read()
	if err != nil {
		return e, err
	}
	list = append(list, e)
	if n := s.max(); len(list) > n {
		list = list[len(list)-n:]
	}
	return e, s.write(list)
}

// List returns the entries, newest first.
func (s *Store) List() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.read()
	for l, r := 0, len(list)-1; l < r; l, r = l+1, r-1 {
		list[l], list[r] = list[r], list[l]
	}
	return list, err
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
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.read()
	if err != nil {
		return err
	}
	out := list[:0]
	for _, e := range list {
		if e.ID != id {
			out = append(out, e)
		}
	}
	return s.write(out)
}

// Clear removes every entry.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.write(nil)
}

func (s *Store) read() ([]Entry, error) {
	b, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e Entry
		if json.Unmarshal([]byte(line), &e) == nil {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *Store) write(list []Entry) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for _, e := range list {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return writePrivate(s.Path, buf.Bytes())
}
