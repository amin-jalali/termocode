package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The HTTP Response page (plan 2.6): a scratch nvim buffer like Find
// Results. Row 1 is the request line, row 2 the status summary, row 3 the
// hairline; from row 4 one of three views (Body, Headers, Raw). Real text
// is what the buffer holds, so /, n, yank and zc work; the chrome is
// virtual text drawn by httpPageLua from the head and the row table.

// HTTP page views.
const (
	httpViewBody = iota
	httpViewHeaders
	httpViewRaw
)

var httpViewNames = []string{"Body", "Headers", "Raw"}

func init() {
	registerNotifyHandler("termocode_http_key", func(m *Model, args []any) tea.Cmd {
		return m.httpPageKey(notifyArgString(args, 0))
	})
}

// httpPage is what the page shows for one run.
type httpPage struct {
	head  map[string]any
	lines []string
	rows  []map[string]any
}

// httpPageHints are the key hints on the hairline row.
func httpPageHints(run *httpRun, sending bool) [][]string {
	var pairs [][2]string
	switch {
	case sending:
		pairs = [][2]string{{"esc", "cancel"}}
	case run != nil && run.resp != nil && run.resp.ErrKind == httpclient.ErrKindTLS && run.by == "user":
		pairs = [][2]string{{"k", "retry insecure"}, {"r", "resend"}, {"h", "history"}, {"q", "close"}}
	default:
		pairs = [][2]string{{"r", "resend"}, {"tab", "view"}, {"c", "copy"}, {"C", "curl"}, {"s", "save"}, {"h", "history"}, {"q", "close"}}
	}
	out := make([][]string, 0, len(pairs))
	for _, p := range pairs {
		k := p[0]
		if len(k) > 1 {
			k = theme.KeyLabel(k)
		}
		out = append(out, []string{k, p[1]})
	}
	return out
}

// buildHTTPPage lays the page out. reveal shows secret header values
// (the user's `x`); sending shows the in-flight summary.
func buildHTTPPage(run *httpRun, view int, reveal, sending bool) httpPage {
	rr := run.rr
	req := rr.Method + " " + ai.Redact(httpclient.RedactURL(rr.URL))
	name := run.label
	if run.file == "" && run.rr.Name == "" {
		name = ""
	}
	by := ""
	if run.by == "ai" {
		by = "by AI"
	}
	head := map[string]any{
		"req": req, "name": name, "by": by, "view": view,
		"chips": httpViewNames, "hints": httpPageHints(run, sending),
		"g":    map[string]string{"hop": httpGlyphHop.String()},
		"st_n": 0, "st_hl": "TermocodeHttpSummary", "syntax": "",
	}
	resp := run.resp
	var summary string
	switch {
	case sending:
		summary = httpGlyphFlight.String() + " sending…"
		if run.progress > 0 {
			summary += " · " + httpclient.FormatSize(run.progress) + " received"
		}
		head["st_hl"] = "TermocodeHttpSummary"
	case resp == nil:
		summary = ""
	case resp.Err != "":
		summary = httpGlyphError.String() + " " + ai.Redact(resp.Err)
		head["st_n"] = len(summary)
		head["st_hl"] = "TermocodeHttpStatusErr"
	default:
		summary = resp.Summary()
		head["st_n"] = len(itoa(resp.Status))
		head["st_hl"] = httpStatusHL(resp.Status)
	}
	head["summary"] = summary
	lines := []string{req, summary, ""}
	var rows []map[string]any
	add := func(kind, text string, extra ...any) {
		lines = append(lines, text)
		r := map[string]any{"l": len(lines), "k": kind}
		for i := 0; i+1 < len(extra); i += 2 {
			r[extra[i].(string)] = extra[i+1]
		}
		rows = append(rows, r)
	}
	plain := func(text string) { lines = append(lines, text) }
	if resp == nil {
		return httpPage{head: head, lines: lines, rows: rows}
	}
	if resp.Err != "" && view == httpViewBody && len(resp.Body) == 0 {
		for _, l := range httpErrorHelp(run) {
			if strings.HasPrefix(l, "  ") && len(l) > 4 && l[3] == ' ' {
				add("key", l, "n", 1)
			} else {
				add("note", l)
			}
		}
		for _, h := range resp.Hops {
			add("hop", "  "+httpGlyphHop.String()+" "+itoa(h.Status)+" → "+httpclient.RedactURL(h.URL), "url", h.URL)
		}
		return httpPage{head: head, lines: lines, rows: rows}
	}
	switch view {
	case httpViewHeaders:
		add("t", "REQUEST")
		for _, h := range resp.ReqHeaders {
			v := h.Value
			if !reveal {
				v = httpclient.RedactHeaderValue(h.Name, v)
			}
			add("h", h.Name+": "+v, "n", len(h.Name)+1)
		}
		plain("")
		for _, h := range resp.Hops {
			add("hop", " "+httpGlyphHop.String()+" "+itoa(h.Status)+" → "+httpclient.RedactURL(h.URL), "url", h.URL)
		}
		proto := resp.Proto
		if proto == "" {
			proto = "HTTP"
		}
		add("t", "RESPONSE  "+proto+" "+itoa(resp.Status), "n", len("RESPONSE"))
		for _, h := range resp.Headers {
			v := h.Value
			if !reveal {
				v = httpclient.RedactHeaderValue(h.Name, v)
			}
			add("h", h.Name+": "+v, "n", len(h.Name)+1)
		}
		plain("")
		add("t", "TIMING  "+httpTimingText(resp.Timing), "n", len("TIMING"))
	case httpViewRaw:
		if resp.IsBinary {
			add("note", httpBinaryNote(resp))
			break
		}
		lines = appendTextLines(lines, resp.TextBytes())
	default:
		if resp.Err == "" {
			for _, h := range resp.Hops {
				add("hop", " "+httpGlyphHop.String()+" "+itoa(h.Status)+" → "+httpclient.RedactURL(h.URL), "url", h.URL)
			}
		}
		switch {
		case resp.IsBinary:
			add("note", httpBinaryNote(resp)+" — s save"+map[bool]string{true: ", o preview", false: ""}[httpIsImage(resp)])
		case len(resp.Body) == 0:
			add("note", "(empty body)")
		default:
			b := run.bodyLines()
			if b.json && len(resp.Body) <= 1<<20 {
				head["syntax"] = "json"
			}
			lines = append(slices.Grow(lines, len(b.lines)), b.lines...)
		}
	}
	return httpPage{head: head, lines: lines, rows: rows}
}

// httpBody is the Body view's text of one answer (pretty JSON when it
// is JSON), laid out once: by the send goroutine, or on the first
// build. Tab back to Body reuses it.
type httpBody struct {
	resp  *httpclient.Response // the answer it was made from
	lines []string
	json  bool // pretty-printed JSON
}

func layoutHTTPBody(resp *httpclient.Response) *httpBody {
	text := resp.TextBytes()
	b := &httpBody{resp: resp}
	if pretty, ok := httpclient.PrettyJSON(text); ok {
		text, b.json = pretty, true
	}
	b.lines = appendTextLines(nil, text)
	return b
}

// bodyLines is run's laid-out body, made again when the answer changed.
func (r *httpRun) bodyLines() *httpBody {
	if r.body == nil || r.body.resp != r.resp {
		r.body = layoutHTTPBody(r.resp)
	}
	return r.body
}

// appendTextLines splits a body into buffer lines (CR LF as LF, NUL as
// a space, no last empty line) and appends them to lines: one copy of
// the text, the line list grown once.
func appendTextLines(lines []string, b []byte) []string {
	s := string(b)
	if strings.Contains(s, "\r\n") {
		s = strings.ReplaceAll(s, "\r\n", "\n")
	}
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", " ")
	}
	s = strings.TrimSuffix(s, "\n")
	lines = slices.Grow(lines, strings.Count(s, "\n")+1)
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			return append(lines, s)
		}
		lines = append(lines, s[:i])
		s = s[i+1:]
	}
}

func httpIsImage(r *httpclient.Response) bool {
	switch r.ContentType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

func httpBinaryNote(r *httpclient.Response) string {
	ct := r.ContentType
	if ct == "" {
		ct = "unknown type"
	}
	return "binary · " + ct + " · " + httpclient.FormatSize(r.Size)
}

func httpTimingText(t httpclient.Timing) string {
	var parts []string
	ms := func(label string, d interface{ Milliseconds() int64 }) {
		parts = append(parts, fmt.Sprintf("%s %d", label, d.Milliseconds()))
	}
	if t.DNS > 0 {
		ms("dns", t.DNS)
	}
	if t.Connect > 0 {
		ms("connect", t.Connect)
	}
	if t.TLS > 0 {
		ms("tls", t.TLS)
	}
	if t.FirstByte > 0 {
		ms("first byte", t.FirstByte)
	}
	ms("total", t.Total)
	return strings.Join(parts, " · ") + " ms"
}

// httpErrorHelp is the body text of a failed send.
func httpErrorHelp(run *httpRun) []string {
	r := run.resp
	var out []string
	switch r.ErrKind {
	case httpclient.ErrKindTLS:
		out = append(out, "The server's certificate is not trusted on this machine.", "")
		if run.by == "user" {
			out = append(out, "  k   retry once without TLS verification (this host, this session)")
		}
	case httpclient.ErrKindTimeout:
		out = append(out, "No answer in time (setting http_timeout).  "+httpTimingText(r.Timing), "")
	case httpclient.ErrKindDial:
		out = append(out, "Could not connect. Check the host and the port, and that the server runs.", "")
	case httpclient.ErrKindRedirects:
		out = append(out, "Too many redirects (setting http_max_redirects).", "")
	case httpclient.ErrKindBlocked:
		out = append(out, "The address is private or special; AI requests may not reach it.", "")
	case httpclient.ErrKindCancelled:
		out = append(out, "The request was cancelled.", "")
	default:
		out = append(out, "The request failed.", "")
	}
	out = append(out, "  r   resend", "  h   history", "  q   close")
	return out
}

// httpPageTmp is the page's buffer name base (a temp path, so sessions
// skip it like Find Results).
func httpPageTmp() string { return filepath.Join(os.TempDir(), "termocode-http-response.txt") }

// isHTTPPagePath says whether a buffer name is the HTTP Response page.
func isHTTPPagePath(p string) bool {
	return p == "HTTP Response" || strings.HasSuffix(p, "/HTTP Response")
}

// httpShowRun opens (or updates) the page for run. sending: the request
// is still in flight (row 2 says so; a previous body stays).
func (m *Model) httpShowRun(run *httpRun, sending bool) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	s := m.httpS()
	if sending && s.shown != nil && s.shown.resp != nil {
		// Keep the previous answer on the page; only rows 1-2 change.
		page := buildHTTPPage(run, s.view, s.reveal, true)
		m.ensureHTTPLua()
		if open, _ := m.httpPageOpen(); open {
			_ = m.nvim.ExecLuaArgs(`local h = ...
if _G._termocode_http_head then _G._termocode_http_head(h) end`, page.head)
			return nil
		}
	}
	if !sending {
		s.shown = run
	}
	page := buildHTTPPage(run, s.view, s.reveal, sending)
	return m.httpOpenPage(page)
}

// httpUpdateHead rewrites rows 1-2 (progress while sending).
func (m *Model) httpUpdateHead() {
	s := m.httpS()
	if m.nvim == nil || s.running == nil {
		return
	}
	page := buildHTTPPage(s.running, s.view, s.reveal, true)
	_ = m.nvim.ExecLuaArgs(`local h = ...
if _G._termocode_http_head then _G._termocode_http_head(h) end`, page.head)
}

// httpPageOpen says whether the page buffer exists and is shown.
func (m *Model) httpPageOpen() (exists, shown bool) {
	if m.nvim == nil {
		return false, false
	}
	r, err := m.nvim.EvalLuaString(`local b = vim.g.termocode_http_buf
if not (b and vim.api.nvim_buf_is_valid(b)) then return 'none' end
return #vim.fn.win_findbuf(b) > 0 and 'shown' or 'hidden'`)
	if err != nil {
		return false, false
	}
	return r != "none", r == "shown"
}

// httpOpenPage fills the page and shows it: in a vertical split when the
// editor pane is at least 120 columns wide, else in the current window.
// Big bodies go in batches of 5 000 lines (httpAppendMsg).
func (m *Model) httpOpenPage(p httpPage) tea.Cmd {
	m.ensureHTTPLua()
	split := m.editorPaneWidth() >= 120
	lines, rows := p.lines, p.rows
	var restL [][]string
	var restR [][]map[string]any
	if len(lines) > httpPageBatch {
		for i := httpPageBatch; i < len(lines); i += httpPageBatch {
			j := min(i+httpPageBatch, len(lines))
			restL = append(restL, lines[i:j])
			var rr []map[string]any
			for _, r := range rows {
				if l, _ := r["l"].(int); l > i && l <= j {
					rr = append(rr, r)
				}
			}
			restR = append(restR, rr)
		}
		lines = lines[:httpPageBatch]
	}
	if err := m.nvim.ExecLuaArgs(`local tmp, split, h, l, r, more = ...
if _G._termocode_http_open then _G._termocode_http_open(tmp, split, h, l, r, more) end`,
		httpPageTmp(), split, p.head, lines, nonNilMapRows(rows), len(restL) > 0); err != nil {
		recordError(fmt.Sprintf("http page: %v", err))
		return nil
	}
	if !split {
		m.focus = FocusEditor
	}
	s := m.httpS()
	s.pageGen++
	if len(restL) == 0 {
		return m.fetchStateCmd()
	}
	gen := s.pageGen
	first := httpAppendMsg{page: gen, lines: restL[0], rows: restR[0], rest: restL[1:], rrows: restR[1:]}
	return tea.Batch(m.fetchStateCmd(), func() tea.Msg { return first })
}

func nonNilMapRows(r []map[string]any) []map[string]any {
	if r == nil {
		return []map[string]any{}
	}
	return r
}

// httpPageAppend adds one batch and schedules the next one, so a key
// press between batches is handled at once.
func (m *Model) httpPageAppend(msg httpAppendMsg) tea.Cmd {
	if m.nvim == nil || msg.page != m.httpS().pageGen {
		return nil
	}
	_ = m.nvim.ExecLuaArgs(`local l, r, last = ...
if _G._termocode_http_append then _G._termocode_http_append(l, r, last) end`, msg.lines, nonNilMapRows(msg.rows), len(msg.rest) == 0)
	if len(msg.rest) == 0 {
		return nil
	}
	next := httpAppendMsg{page: msg.page, lines: msg.rest[0], rows: msg.rrows[0], rest: msg.rest[1:], rrows: msg.rrows[1:]}
	return func() tea.Msg { return next }
}

// httpClosePage closes the page (its split window too).
func (m *Model) httpClosePage() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	_ = m.nvim.ExecLua(`local b = vim.g.termocode_http_buf
if b and vim.api.nvim_buf_is_valid(b) then
  for _, w in ipairs(vim.fn.win_findbuf(b)) do
    if #vim.api.nvim_tabpage_list_wins(0) > 1 then pcall(vim.api.nvim_win_close, w, true) end
  end
  pcall(vim.cmd, 'bwipeout ' .. b) -- not bdelete: a reused unlisted buffer loses its filetype
end`)
	return m.fetchStateCmd()
}

// httpRedrawPage rebuilds the shown run in the current view.
func (m *Model) httpRedrawPage() tea.Cmd {
	s := m.httpS()
	if s.shown == nil {
		return nil
	}
	return m.httpOpenPage(buildHTTPPage(s.shown, s.view, s.reveal, false))
}

// httpPageKey runs a key pressed on the page (buffer-local maps in
// httpPageLua notify termocode_http_key).
func (m *Model) httpPageKey(key string) tea.Cmd {
	s := m.httpS()
	switch {
	case key == "r":
		return m.httpResend()
	case key == "tab":
		s.view = (s.view + 1) % len(httpViewNames)
		return m.httpRedrawPage()
	case key == "1" || key == "2" || key == "3":
		s.view = int(key[0] - '1')
		return m.httpRedrawPage()
	case strings.HasPrefix(key, "view:"):
		for i, n := range httpViewNames {
			if strings.EqualFold(n, key[5:]) || (n == "Headers" && key[5:] == "Hdrs") {
				s.view = i
			}
		}
		return m.httpRedrawPage()
	case key == "c":
		return m.httpCopyBody()
	case key == "C":
		return m.httpCopyCurl(s.shown)
	case key == "s":
		return m.httpSaveBodyPrompt()
	case key == "S":
		return m.httpSaveRequestPrompt()
	case key == "h":
		return m.openHTTPHistory()
	case key == "e":
		return m.openHTTPEnvPicker()
	case key == "k":
		return m.httpRetryInsecure()
	case key == "x":
		s.reveal = !s.reveal
		return m.httpRedrawPage()
	case key == "o":
		return m.httpPreviewImage()
	case key == "esc":
		if s.running != nil {
			return m.httpCancel()
		}
		return m.httpClosePage()
	case key == "q":
		return m.httpClosePage()
	case key == "jump":
		return m.httpJumpToRequest()
	case strings.HasPrefix(key, "hop:"):
		return m.copyWithToast(key[4:], "Copied URL")
	}
	return nil
}

// httpJumpToRequest opens the .http file at the shown request.
func (m *Model) httpJumpToRequest() tea.Cmd {
	s := m.httpS()
	if s.shown == nil || s.shown.file == "" || m.nvim == nil {
		return nil
	}
	m.ensureEditorWindowCurrent()
	_ = m.nvim.ExecLuaArgs(`local l, f = ...
vim.cmd('edit +' .. l .. ' ' .. vim.fn.fnameescape(f))`, max(s.shown.line, 1), s.shown.file)
	m.focus = FocusEditor
	return m.fetchStateCmd()
}

// httpPreviewImage writes an image body to a temp file and opens the
// image card.
func (m *Model) httpPreviewImage() tea.Cmd {
	s := m.httpS()
	if s.shown == nil || s.shown.resp == nil || !httpIsImage(s.shown.resp) {
		return nil
	}
	ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}[s.shown.resp.ContentType]
	f, err := os.CreateTemp("", "termocode-http-*"+ext)
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Preview failed", err.Error())
	}
	_, _ = f.Write(s.shown.resp.Body)
	_ = f.Close()
	cmd := m.openImagePreview(f.Name())
	m.imagePrev.tmp = true // removed when the preview closes (U16)
	return cmd
}

// httpRetryInsecure resends the shown request once without TLS checks
// for its host (this session only). User sends only, never AI ones.
func (m *Model) httpRetryInsecure() tea.Cmd {
	s := m.httpS()
	r := s.shown
	if r == nil || r.resp == nil || r.resp.ErrKind != httpclient.ErrKindTLS || r.by != "user" {
		return nil
	}
	if s.running != nil {
		return m.pushToastDetail(toast.Warn, "A request is still running", "")
	}
	host := httpclient.HostKey(r.rr.URL)
	s.insecure[host] = true
	run := &httpRun{rr: r.rr, file: r.file, line: r.line, label: r.label, env: r.env}
	return m.startHTTPRun(run, httpclient.Hooks{Insecure: true})
}

// httpCopyBody copies the shown body (pretty JSON as on the page).
func (m *Model) httpCopyBody() tea.Cmd {
	s := m.httpS()
	if s.shown == nil || s.shown.resp == nil || s.shown.resp.IsBinary {
		return m.pushToastDetail(toast.Warn, "Nothing to copy", "")
	}
	text := s.shown.resp.Text()
	if p, ok := httpclient.PrettyJSON([]byte(text)); ok {
		text = string(p)
	}
	return m.copyWithToast(text, "Copied response body")
}
