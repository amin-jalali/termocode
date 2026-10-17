package app

// HTTP client for .http / .rest files (VS Code REST Client style). The
// request is the .http file in the editor (http_buffer.go), the answer
// is an nvim page next to it (http_page.go), commands and pickers are in
// http_commands.go and the AI tools in ai_http_tools.go. The engine is
// internal/httpclient (pure Go, no curl).
//
// This file holds the state, the settings and the send machinery.
// Sends run in a tea.Cmd goroutine and report back with httpEventMsg
// (progress every 256 KB, then the answer): no timers, idle costs 0.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── settings (config.json keys; settingsConfig keeps unknown keys) ──────

type httpSettings struct {
	Timeout      *int  `json:"http_timeout,omitempty"` // seconds
	MaxRedirects *int  `json:"http_max_redirects,omitempty"`
	MaxBodyMB    *int  `json:"http_max_body_mb,omitempty"`
	AIMaxKB      *int  `json:"http_ai_max_kb,omitempty"`
	TLSInsecure  *bool `json:"http_tls_insecure,omitempty"`
	CookieJar    *bool `json:"http_cookie_jar,omitempty"`
	HistoryMax   *int  `json:"http_history_max,omitempty"`
}

// HTTP setting defaults.
const (
	httpDefaultTimeout   = 30
	httpDefaultAIMaxKB   = 32
	httpDefaultMaxBodyMB = 20
	httpRingMax          = 20 // responses kept in memory
	httpRingBodies       = 3  // of those, the newest keep their full body
	httpPageBatch        = 5000
)

func loadHTTPSettings() httpSettings {
	var s httpSettings
	path, err := settingsConfigPath()
	if err != nil {
		return s
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

func (s httpSettings) timeout() time.Duration {
	n := intOr(s.Timeout, httpDefaultTimeout)
	if n <= 0 {
		n = httpDefaultTimeout
	}
	return time.Duration(n) * time.Second
}

func (s httpSettings) maxRedirects() int {
	n := intOr(s.MaxRedirects, httpclient.DefaultMaxRedirects)
	if n <= 0 {
		n = httpclient.DefaultMaxRedirects
	}
	return n
}

func (s httpSettings) maxBody() int64 {
	n := intOr(s.MaxBodyMB, httpDefaultMaxBodyMB)
	if n <= 0 {
		n = httpDefaultMaxBodyMB
	}
	return int64(n) << 20
}

func (s httpSettings) aiMaxBytes() int {
	n := intOr(s.AIMaxKB, httpDefaultAIMaxKB)
	if n <= 0 {
		n = httpDefaultAIMaxKB
	}
	return n << 10
}

func (s httpSettings) cookieJar() bool { return s.CookieJar == nil || *s.CookieJar }

func (s httpSettings) tlsInsecure() bool { return s.TLSInsecure != nil && *s.TLSInsecure }

func (s httpSettings) historyMax() int {
	n := intOr(s.HistoryMax, httpclient.DefaultHistoryMax)
	if n <= 0 {
		n = httpclient.DefaultHistoryMax
	}
	return n
}

// saveHTTPSetting writes one http_* key into config.json (other keys
// kept).
func saveHTTPSetting(key string, value any) error {
	path, err := settingsConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	merged := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &merged)
	}
	merged[key] = value
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// ── files under ~/.config/termocode/http/ ───────────────────────────────

func httpConfigDir() string {
	dir, err := ai.ConfigDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "termocode-http")
	}
	return filepath.Join(dir, "http")
}

func httpHistoryPath() string { return filepath.Join(httpConfigDir(), "history.jsonl") }
func httpStatePath() string   { return filepath.Join(httpConfigDir(), "state.json") }
func httpSavedPath() string   { return filepath.Join(httpConfigDir(), "saved.http") }

// httpHistoryStore is the history file with the current cap; every text
// passes ai.Redact before it reaches the disk.
func httpHistoryStore() *httpclient.Store {
	return &httpclient.Store{Path: httpHistoryPath(), Max: loadHTTPSettings().historyMax(), Redact: ai.Redact}
}

// httpSelectedEnv is the environment chosen for the workspace root.
func httpSelectedEnv(root string) string {
	st, _ := httpclient.LoadState(httpStatePath())
	return st.Env(root)
}

func httpSetSelectedEnv(root, env string) error {
	st, _ := httpclient.LoadState(httpStatePath())
	st.SetEnv(root, env)
	return st.Save(httpStatePath())
}

// ── state ───────────────────────────────────────────────────────────────

// httpRun is one send: the resolved request and, once done, the answer.
type httpRun struct {
	id       int
	rr       httpclient.Resolved
	resp     *httpclient.Response
	file     string // abs path of the .http file ("" = quick request)
	line     int    // request line in file
	label    string // request name or "METHOD /path"
	env      string
	by       string // "user" or "ai"
	at       time.Time
	progress int64
}

// httpState is the HTTP client's per-app state (Model.http).
type httpState struct {
	mu        sync.Mutex // guards client
	client    *httpclient.Client
	clientSig string

	gen     int
	cancel  context.CancelFunc
	running *httpRun
	last    *httpRun
	ring    []*httpRun
	nextID  int

	// lens results per .http file and request label.
	lens map[string]httpLens
	// insecure: hosts the user retried with `k` this session.
	insecure map[string]bool

	pageGen int  // bumps on every page fill; stale batches are dropped
	view    int  // 0 body, 1 headers, 2 raw
	reveal  bool // x: show secret header values on the page
	shown   *httpRun

	pending     *httpRun // waits for "Send anyway?"
	pendingHook httpclient.Hooks
	gitWarned   map[string]bool
	histIDs     map[string]string // picker item → history entry id
	saved       []httpclient.Request
	pickReqs    []httpPickReq
	envRoot     string
	saveBody    []byte
	copyRun     *httpRun
	importInto  string // .http file the next curl import appends to
}

type httpLens struct {
	text string // "200 · 118 ms"
	hl   string // highlight of the status number
	n    int    // bytes of the status number in text
}

type httpPickReq struct {
	file string
	line int
}

func newHTTPState() *httpState {
	return &httpState{lens: map[string]httpLens{}, insecure: map[string]bool{}, gitWarned: map[string]bool{}}
}

// httpS returns the HTTP state, made on first use.
func (m *Model) httpS() *httpState {
	if m.http == nil {
		m.http = newHTTPState()
	}
	return m.http
}

// httpClientFor returns the shared client (cookie jar per app session),
// rebuilt when the settings change.
func (s *httpState) httpClient(set httpSettings) *httpclient.Client {
	sig := fmt.Sprint(set.timeout(), set.maxRedirects(), set.maxBody(), set.cookieJar())
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil || s.clientSig != sig {
		if s.client != nil {
			s.client.Close()
		}
		s.client = httpclient.New(httpclient.Options{
			Timeout:      set.timeout(),
			MaxRedirects: set.maxRedirects(),
			MaxBody:      set.maxBody(),
			CookieJar:    set.cookieJar(),
			UserAgent:    "termocode/" + strings.TrimPrefix(termocodeVersion, "v"),
		})
		s.clientSig = sig
	}
	return s.client
}

// remember puts a finished run in the ring (bodies kept for the newest
// few only, risk 4 of the plan).
func (s *httpState) remember(r *httpRun) {
	s.last = r
	s.ring = append(s.ring, r)
	if len(s.ring) > httpRingMax {
		s.ring = s.ring[len(s.ring)-httpRingMax:]
	}
	for i := 0; i < len(s.ring)-httpRingBodies; i++ {
		if old := s.ring[i]; old.resp != nil && len(old.resp.Body) > 64<<10 && old != s.shown {
			cp := *old.resp
			cp.Body = cp.Body[:64<<10]
			cp.Truncated = true
			old.resp = &cp
		}
	}
}

// findRun returns the run with id from the ring (0 = last).
func (s *httpState) findRun(id int) *httpRun {
	if id == 0 {
		return s.last
	}
	for _, r := range s.ring {
		if r.id == id {
			return r
		}
	}
	return nil
}

// ── resolving ───────────────────────────────────────────────────────────

// httpRootFor is the workspace root that holds path (the cwd when none).
func httpRootFor(path string) string {
	best := ""
	for _, r := range workspaceRoots() {
		rel, err := filepath.Rel(r, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if len(r) > len(best) {
			best = r
		}
	}
	if best == "" {
		best = aiWorkspaceRoot()
	}
	return best
}

// httpSecretLookup reads {{$secret NAME}} from ai/credentials.json (the
// store MCP uses for ${secret:NAME}).
func httpSecretLookup(name string) (string, bool) {
	path, err := ai.CredentialsPath()
	if err != nil {
		return "", false
	}
	c, err := ai.LoadCredentials(path)
	if err != nil {
		return "", false
	}
	v, ok := c.MCPSecrets[name]
	return v, ok
}

// httpResolver builds the resolver for a .http file (or a quick request
// when file is ""): its variables, the env files up to the workspace
// root and the selected environment. env "" uses the selected one;
// "-" means none.
func httpResolver(file string, fileVars []httpclient.Var, env string) *httpclient.Resolver {
	dir := aiWorkspaceRoot()
	if file != "" {
		dir = filepath.Dir(file)
	}
	root := httpRootFor(dir)
	if env == "" {
		env = httpSelectedEnv(root)
	} else if env == "-" {
		env = ""
	}
	return &httpclient.Resolver{
		FileVars: fileVars,
		Env:      env,
		Envs:     httpclient.LoadEnvs(dir, root),
		Secret:   httpSecretLookup,
		OnSecret: ai.RegisterSecret,
	}
}

// ── sending ─────────────────────────────────────────────────────────────

// httpEvent is one step of a send: progress or the answer.
type httpEvent struct {
	progress int64
	resp     *httpclient.Response
}

// httpEventMsg carries one httpEvent into Update.
type httpEventMsg struct {
	gen int
	ch  <-chan httpEvent
	ev  httpEvent
	run *httpRun
}

func waitHTTPEvent(gen int, run *httpRun, ch <-chan httpEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return httpEventMsg{gen: gen, ch: ch, ev: ev, run: run}
	}
}

// httpAppendMsg sends the next batch of page lines.
type httpAppendMsg struct {
	page  int
	lines []string
	rows  []map[string]any
	rest  [][]string
	rrows [][]map[string]any
}

// httpRecordHistory appends a finished run to history.jsonl.
func httpRecordHistory(r *httpRun) {
	file := r.file
	if file != "" {
		if rel, err := filepath.Rel(httpRootFor(file), file); err == nil && !strings.HasPrefix(rel, "..") {
			file = filepath.ToSlash(rel)
		}
	}
	e := httpclient.NewEntry(r.rr.Template, file, r.env, r.by, r.resp)
	if r.file != "" {
		e.Name = r.label
	}
	if err := httpHistoryStore().Append(e); err != nil {
		recordError("http history: " + ai.Redact(err.Error()))
	}
}

// startHTTPRun sends run as the user's request: the lens and the status
// chip say "sending", the page opens on the answer.
func (m *Model) startHTTPRun(run *httpRun, hooks httpclient.Hooks) tea.Cmd {
	s := m.httpS()
	set := loadHTTPSettings()
	client := s.httpClient(set)
	s.nextID++
	run.id = s.nextID
	run.by = "user"
	run.at = time.Now()
	if set.tlsInsecure() {
		hooks.Insecure = true
	}
	if u := httpclient.HostKey(run.rr.URL); u != "" && s.insecure[u] {
		hooks.Insecure = true
	}
	s.gen++
	gen := s.gen
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running = run
	ch := make(chan httpEvent, 8)
	hooks.Progress = func(n int64) {
		select {
		case ch <- httpEvent{progress: n}:
		default:
		}
	}
	snap := *run // the goroutine never touches run itself
	go func() {
		resp := client.Send(ctx, snap.rr, hooks)
		snap.resp = resp
		httpRecordHistory(&snap)
		ch <- httpEvent{resp: resp}
		close(ch)
	}()
	m.httpRefreshLens(run.file)
	m.httpShowRun(run, true)
	return waitHTTPEvent(gen, run, ch)
}

// handleHTTPMsg handles the HTTP client's messages. ok=false: not ours.
func (m *Model) handleHTTPMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case httpEventMsg:
		s := m.httpS()
		if msg.gen != s.gen || s.running != msg.run {
			return nil, true
		}
		if msg.ev.resp == nil {
			msg.run.progress = msg.ev.progress
			m.httpUpdateHead()
			return waitHTTPEvent(msg.gen, msg.run, msg.ch), true
		}
		msg.run.resp = msg.ev.resp
		return m.httpFinish(msg.run), true
	case httpAppendMsg:
		return m.httpPageAppend(msg), true
	case httpPrivateEnvCheckMsg:
		title := "Secrets file not git-ignored"
		if msg.state == httpGitTracked {
			title = "Secrets file tracked by git"
		}
		return m.pushToastDetail(toast.Warn, title,
			filepath.ToSlash(msg.rel)+" holds tokens. HTTP: Open Private Environment File offers to add it to .gitignore."), true
	}
	return nil, false
}

// httpFinish records a finished user run and shows it.
func (m *Model) httpFinish(run *httpRun) tea.Cmd {
	s := m.httpS()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.running = nil
	if run.resp.ErrKind == httpclient.ErrKindCancelled && s.shown != nil && s.shown != run && s.shown.resp != nil {
		// Keep the previous body; only row 2 says cancelled.
		prev := *s.shown
		r := *s.shown.resp
		r.Err, r.ErrKind = run.resp.Err, run.resp.ErrKind
		prev.resp = &r
		s.remember(run)
		m.httpRecordLens(run)
		m.httpRefreshLens(run.file)
		return m.httpShowRun(&prev, false)
	}
	s.remember(run)
	m.httpRecordLens(run)
	m.httpRefreshLens(run.file)
	return m.httpShowRun(run, false)
}

// httpRecordLens remembers the result shown in the lens of the request.
func (m *Model) httpRecordLens(run *httpRun) {
	if run.file == "" || run.resp == nil {
		return
	}
	s := m.httpS()
	var l httpLens
	if run.resp.Err != "" {
		l = httpLens{text: "ERR", hl: "TermocodeHttpLensErr", n: 3}
	} else {
		code := itoa(run.resp.Status)
		hl := strings.Replace(httpStatusHL(run.resp.Status), "Status", "Lens", 1) // no bold in the editor
		l = httpLens{text: code + " · " + httpclient.FormatDuration(run.resp.Elapsed), hl: hl, n: len(code)}
	}
	s.lens[run.file+"\x00"+run.label] = l
}

// httpCancel stops the running request (Esc on the page, palette).
func (m *Model) httpCancel() tea.Cmd {
	s := m.httpS()
	if s.running == nil || s.cancel == nil {
		return m.pushToastDetail(toast.Info, "No request is running", "")
	}
	s.cancel()
	return nil
}

// httpStatusHL is the highlight of a status number: 2xx green, 3xx info,
// 4xx warning, 5xx and errors error (plan 2.7).
func httpStatusHL(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "TermocodeHttpStatusOK"
	case status >= 300 && status < 400:
		return "TermocodeHttpStatusInfo"
	case status >= 400 && status < 500:
		return "TermocodeHttpStatusWarn"
	}
	return "TermocodeHttpStatusErr"
}

// httpStatusTexts is the status-bar chip: "⇄ sending" while a request
// runs, "⇄ GET 200 · 118 ms" after, nothing before the first send. Ext
// markup ({{token}}), like extension items.
func (m Model) httpStatusTexts() []string {
	s := m.http
	if s == nil {
		return nil
	}
	g := httpGlyphFlight.String()
	if s.running != nil {
		return []string{"{{accent}}" + g + "{{/}} sending"}
	}
	r := s.last
	if r == nil || r.resp == nil {
		return nil
	}
	if r.resp.Err != "" {
		return []string{"{{error}}" + g + "{{/}} " + r.rr.Method + " {{error}}ERR{{/}}"}
	}
	tok := map[string]string{"TermocodeHttpStatusOK": "success", "TermocodeHttpStatusInfo": "info", "TermocodeHttpStatusWarn": "warning", "TermocodeHttpStatusErr": "error"}[httpStatusHL(r.resp.Status)]
	text := "{{accent}}" + g + "{{/}} " + r.rr.Method + " {{" + tok + "}}" + itoa(r.resp.Status) + "{{/}}"
	if m.w >= 100 {
		text += " · " + httpclient.FormatDuration(r.resp.Elapsed)
	}
	return []string{text}
}

// Glyphs from the theme table (1 cell, ASCII fallbacks; plan 2.7).
var (
	httpGlyphSend   = theme.GlyphSend
	httpGlyphFlight = theme.GlyphSending
	httpGlyphError  = theme.GlyphFail
	httpGlyphHop    = theme.GlyphRedirect
)
