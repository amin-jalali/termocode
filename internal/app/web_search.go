package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/search"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Search from the web (WEB-24, web design 2.7). search.run streams its
// hits as search.batch events and answers with a summary when it ends. It
// runs off the UI thread with the request's context, inside the
// workspace only (relative paths; globs that leave it are refused by the
// args rules; links are not followed). One search per connection: a new
// one stops the old, search.stop stops it, and it stops by itself after
// webSearchTimeout, max hits or webSearchMaxBytes of text.

func init() {
	webOps[webproto.OpSearchRun] = (*Model).webSearchRun
	webOps[webproto.OpSearchStop] = (*Model).webSearchStop
	webOps[webproto.OpSearchOpen] = (*Model).webSearchOpen
}

// Tests swap these.
var (
	webSearchStream   = search.Stream
	webSearchTimeout  = webproto.SearchTimeout
	webSearchMaxBytes = webproto.MaxSearchBytes
)

// Why a search ended early (context causes).
var (
	errSearchReplaced = errors.New("replaced")
	errSearchStopped  = errors.New("stopped")
	errSearchTimeout  = errors.New("timeout")
	errSearchDone     = errors.New("done") // max hits or the byte cap: the search ends itself
)

// webSearchRun is one running search.
type webSearchRun struct{ cancel context.CancelCauseFunc }

// webSearches are the running searches by connection.
type webSearches struct {
	mu   sync.Mutex
	runs map[*webConn]*webSearchRun
}

// start makes r c's search, stopping the one before.
func (s *webSearches) start(c *webConn, r *webSearchRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.runs[c]; old != nil {
		old.cancel(errSearchReplaced)
	}
	if s.runs == nil {
		s.runs = map[*webConn]*webSearchRun{}
	}
	s.runs[c] = r
}

// stop stops c's search; false when none runs.
func (s *webSearches) stop(c *webConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.runs[c]
	if r == nil {
		return false
	}
	delete(s.runs, c)
	r.cancel(errSearchStopped)
	return true
}

// done forgets r if it is still c's search.
func (s *webSearches) done(c *webConn, r *webSearchRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runs[c] == r {
		delete(s.runs, c)
	}
}

func (m *Model) webSearchRun(call *webCall) tea.Cmd {
	a := call.args.(*webproto.SearchRunArgs)
	root, err := m.rootDir()
	if err != nil {
		call.fail(webproto.CodeConflict, "No workspace is open on the laptop.")
		return nil
	}
	max := a.Max
	if max == 0 {
		max = webproto.SearchDefaultMax
	}
	opts := search.Options{CaseSensitive: a.Case, WholeWord: a.Word, Regex: a.Regex,
		Include: search.SplitGlobs(a.Include), Exclude: search.SplitGlobs(a.Exclude),
		Context: webproto.SearchContext, MaxResults: max}
	m.webAuditOp(call, a.Query, "ok")
	ctx, cancel := context.WithCancelCause(call.ctx)
	ctx, stopTimer := context.WithTimeoutCause(ctx, webSearchTimeout, errSearchTimeout)
	run := &webSearchRun{cancel: cancel}
	h := m.web
	h.search.start(call.conn, run)
	call.later()
	crash.GoWith("web search", func() {
		defer h.search.done(call.conn, run)
		defer cancel(nil)
		defer stopTimer()
		s := webSearcher{call: call, root: root, max: max, files: map[string]bool{}, start: time.Now()}
		s.run(ctx, cancel, a.Query, opts)
	}, func() {
		cancel(nil)
		h.search.done(call.conn, run)
		call.fail(webproto.CodeInternal, "Something went wrong.")
	})
	return nil
}

// webSearchStop stops the connection's search (it answers with its
// summary); with none running it is fine too.
func (m *Model) webSearchStop(call *webCall) tea.Cmd {
	m.web.search.stop(call.conn)
	call.reply(nil)
	return nil
}

// webSearchOpen checks that a hit's file is a file of the workspace (no
// link leads out); the page then opens it at the line.
func (m *Model) webSearchOpen(call *webCall) tea.Cmd {
	a := call.args.(*webproto.SearchOpenArgs)
	root, err := m.rootDir()
	if err == nil && !webGitInside(root, a.Path) {
		err = errors.New("outside")
	}
	if err == nil {
		var abs string
		if abs, err = (ai.Workspace{Root: root}).Resolve(a.Path); err == nil {
			var st os.FileInfo
			if st, err = os.Lstat(abs); err == nil && !st.Mode().IsRegular() {
				err = errors.New("not a file")
			}
		}
	}
	if err != nil {
		call.fail(webproto.CodeNotFound, "That file is not in the workspace now.")
		return nil
	}
	call.reply(nil)
	return nil
}

// webSearcher is one search's state on its goroutine.
type webSearcher struct {
	call    *webCall
	root    string
	max     int
	sent    int
	bytes   int
	files   map[string]bool
	limited bool // stopped by the byte cap
	start   time.Time
}

func (s *webSearcher) run(ctx context.Context, cancel context.CancelCauseFunc, query string, opts search.Options) {
	var sum search.Summary
	var serr error
	done, full := false, false
	for b := range webSearchStream(ctx, []string{s.root}, query, opts) {
		if b.Done {
			sum, serr, done = b.Summary, b.Err, true
			break
		}
		if !s.send(b.Results) {
			full = true // max hits or the byte cap: stop the search
			cancel(errSearchDone)
			break
		}
	}
	stopped := ""
	switch cause := context.Cause(ctx); {
	case s.limited:
		stopped = "limit"
	case full || errors.Is(cause, errSearchDone):
	case errors.Is(cause, errSearchReplaced):
		stopped = "replaced"
	case errors.Is(cause, errSearchStopped):
		stopped = "stopped"
	case errors.Is(cause, errSearchTimeout):
		stopped = "timeout"
	case ctx.Err() != nil:
		s.call.fail(webproto.CodeGone, "The connection ended.")
		return
	case done && serr != nil:
		line, _, _ := strings.Cut(serr.Error(), "\n")
		s.call.fail(webproto.CodeBadRequest, "The search failed: "+line)
		return
	}
	s.call.reply(webproto.SearchSummary{Files: len(s.files), Matches: s.sent,
		Truncated: stopped != "" || full || sum.Total > s.sent, Ms: time.Since(s.start).Milliseconds(), Stopped: stopped})
}

// send sends results as batches; false when the search must stop (max
// hits sent, or the byte cap reached).
func (s *webSearcher) send(rs []search.Result) bool {
	var batch []webproto.SearchHit
	flush := func() {
		if len(batch) == 0 {
			return
		}
		data, err := webData(webproto.SearchBatch{ID: s.call.id, Results: batch})
		if err == nil {
			s.call.conn.send(&webproto.Event{T: webproto.KindEv, Kind: webproto.EventSearchBatch, Data: data})
		}
		batch = nil
	}
	defer flush()
	for _, r := range rs {
		if s.sent >= s.max {
			return false
		}
		rel, err := filepath.Rel(s.root, r.Path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		h := webSearchHit(filepath.ToSlash(rel), r)
		size := len(h.Text)
		for _, l := range h.Before {
			size += len(l)
		}
		for _, l := range h.After {
			size += len(l)
		}
		if s.bytes+size > webSearchMaxBytes {
			s.limited = true
			return false
		}
		s.bytes += size
		s.sent++
		s.files[h.Path] = true
		batch = append(batch, h)
		if len(batch) == webproto.MaxSearchBatch {
			flush()
		}
	}
	return s.sent < s.max
}

// webSearchHit makes one hit: texts cut at MaxSearchText bytes on a rune
// edge, the matches as UTF-16 offsets inside the cut text.
func webSearchHit(path string, r search.Result) webproto.SearchHit {
	text := webCutText(r.Preview)
	h := webproto.SearchHit{Path: path, Line: r.Line, Col: r.Col, Text: text}
	for _, mr := range r.Matches {
		if mr.Start < 0 || mr.End > len(text) || mr.Start >= mr.End {
			continue
		}
		h.Marks = append(h.Marks, webproto.SearchMark{S: webUTF16Len(text[:mr.Start]), E: webUTF16Len(text[:mr.End])})
	}
	for _, c := range r.Before {
		h.Before = append(h.Before, webCutText(c.Text))
	}
	for _, c := range r.After {
		h.After = append(h.After, webCutText(c.Text))
	}
	return h
}

// webCutText cuts s at MaxSearchText bytes, on a rune edge.
func webCutText(s string) string {
	if len(s) <= webproto.MaxSearchText {
		return s
	}
	cut := webproto.MaxSearchText
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// webUTF16Len is the length of s in UTF-16 code units (a JavaScript
// string's length).
func webUTF16Len(s string) int {
	n := 0
	for _, r := range s {
		n += len(utf16.Encode([]rune{r}))
	}
	return n
}
