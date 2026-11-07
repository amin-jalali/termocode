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
	"bytes"
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
	"github.com/amin-jalali/termocode/internal/cfgfile"
	"github.com/amin-jalali/termocode/internal/crash"
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
	httpPageBatch        = 20000
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
	return cfgfile.Update(path, func(merged map[string]any) error {
		merged[key] = value
		return nil
	})
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
func httpGlobalsPath() string { return filepath.Join(httpConfigDir(), "globals.json") }

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
	body     *httpBody // the Body view's lines (made once per resp)
	live     []byte    // the newest text of a stream while it runs (API-16)
	// tests are the @assert / @pre / @post results of this send (API-8).
	tests []httpclient.AssertResult
	// prev is the answer the same request got the time before (the
	// Diff view compares with it); nil when there is none or it was big.
	prev *httpclient.Response
}

// httpState is the HTTP client's per-app state (Model.http).
type httpState struct {
	mu        sync.Mutex // guards client
	client    *httpclient.Client
	clientSig string

	cookieMu   sync.Mutex // guards cookies
	cookies    *httpclient.CookieStore
	cookieEnv  string // the cookies picker's environment
	cookieList []httpclient.Cookie
	cookieEdit httpclient.Cookie

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

	pending *httpRun // waits for "Send anyway?"
	// oauthWait is the run that waits for its OAuth 2.0 token.
	oauthWait *httpOAuthWait
	// promptWait collects the `# @prompt` values of a request.
	promptWait *httpPromptWait

	// chainWait waits for the "Send login first?" card; chainNext are
	// the requests to send once their dependency answered (a stack:
	// the top waits for the run that is sending now).
	chainWait *httpChainStep
	chainNext []httpChainStep
	// chainSnap holds the newest good answer per file + request name,
	// for the AI tools (read off the Update goroutine; guarded).
	chainMu     sync.Mutex
	chainSnap   map[string]*httpclient.Response
	pendingHook httpclient.Hooks
	gitWarned   map[string]bool
	histIDs     map[string]string // picker item → history entry id
	saved       []httpclient.Request
	pickReqs    []httpPickReq
	envRoot     string
	saveBody    []byte
	copyRun     *httpRun
	copyLang    string // Copy as Code… language of copyRun ("" = curl)
	importInto  string // .http file the next curl import appends to
	bufCache    *httpBufCache
	fields      httpFieldsState // the params and headers table (http_fields.go)

	// GraphQL (http_graphql.go): schemas per request URL, the name typed
	// before the completion picker, the schema the docs picker lists.
	gqlSchemas map[string]*httpclient.GQLSchema
	gqlPrefix  string
	gqlDocs    *httpclient.GQLSchema

	mqttRunning bool        // an MQTT session runs (http_mqtt.go)
	proxyReq    httpPickReq // the request HTTP: Set Request Proxy… edits

	// The web's request (one at a time) and the hosts the laptop allowed
	// the web to send to in this session (web_http.go).
	webRun    *httpRun
	webCancel context.CancelFunc
	webHosts  map[string]bool

	// The WebSocket session (http_ws.go, API-17); wsShown: the page
	// shows it rather than an HTTP answer.
	ws      *httpWSState
	wsGen   int
	wsShown bool

	// The local mock server (API-22, http_mock.go).
	mockAddr   string // host:port, "" = none running
	mockFile   string // the .http / OpenAPI file it serves
	mockCancel context.CancelFunc
	mockSrv    *httpclient.Mock
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
	return &httpState{lens: map[string]httpLens{}, insecure: map[string]bool{}, gitWarned: map[string]bool{}, webHosts: map[string]bool{}}
}

// httpS returns the HTTP state, made on first use.
func (m *Model) httpS() *httpState {
	if m.http == nil {
		m.http = newHTTPState()
	}
	return m.http
}

// httpClientFor returns the shared client (cookies per environment,
// saved in http/cookies.json),
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
			Cookies:      s.cookieStore(),
			UserAgent:    "termocode/" + strings.TrimPrefix(termocodeVersion, "v"),
		})
		s.clientSig = sig
	}
	return s.client
}

// remember puts a finished run in the ring (bodies kept for the newest
// few only, risk 4 of the plan).
func (s *httpState) remember(r *httpRun) {
	if r.prev == nil {
		r.prev = s.previousAnswer(r)
	}
	s.last = r
	s.ring = append(s.ring, r)
	if r.rr.Template.Name != "" && r.resp != nil && r.resp.Err == "" {
		s.chainMu.Lock()
		if s.chainSnap == nil {
			s.chainSnap = map[string]*httpclient.Response{}
		}
		s.chainSnap[r.file+"\x00"+r.rr.Template.Name] = r.resp
		s.chainMu.Unlock()
	}
	if len(s.ring) > httpRingMax {
		s.ring = s.ring[len(s.ring)-httpRingMax:]
	}
	for i := 0; i < len(s.ring)-httpRingBodies; i++ {
		if old := s.ring[i]; old.resp != nil && len(old.resp.Body) > 64<<10 && old != s.shown {
			cp := *old.resp
			cp.Body = cp.Body[:64<<10]
			cp.Truncated = true
			old.resp = &cp
			old.body = nil
			old.prev = nil
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

// chainSource answers {{name.response...}} for requests in file: the
// newest run of the request called name there that got an answer (a
// failed send does not count). Only call it on the Update goroutine.
func (s *httpState) chainSource(file string) func(string) (*httpclient.Response, bool) {
	return func(name string) (*httpclient.Response, bool) {
		for i := len(s.ring) - 1; i >= 0; i-- {
			r := s.ring[i]
			if r.file == file && r.rr.Template.Name == name && r.resp != nil && r.resp.Err == "" {
				return r.resp, true
			}
		}
		return nil, false
	}
}

// chainSnapshot is chainSource for the AI tools: safe on any
// goroutine (it reads the guarded copy, not the ring).
func (s *httpState) chainSnapshot(file string) func(string) (*httpclient.Response, bool) {
	return func(name string) (*httpclient.Response, bool) {
		s.chainMu.Lock()
		defer s.chainMu.Unlock()
		r, ok := s.chainSnap[file+"\x00"+name]
		return r, ok && r != nil
	}
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

// httpLoadCredentials reads the credentials store (a test hook).
var httpLoadCredentials = ai.LoadCredentials

// httpSecretLookup returns a {{$secret NAME}} lookup in
// ai/credentials.json (the store MCP uses for ${secret:NAME}). The
// store is read once, on the first lookup, so one resolver (one send,
// one buffer refresh) never reads it per placeholder; a new lookup
// reads it again.
func httpSecretLookup() func(name string) (string, bool) {
	var (
		once    sync.Once
		secrets map[string]string
	)
	return func(name string) (string, bool) {
		once.Do(func() {
			path, err := ai.CredentialsPath()
			if err != nil {
				return
			}
			if c, err := httpLoadCredentials(path); err == nil {
				secrets = c.MCPSecrets
			}
		})
		v, ok := secrets[name]
		return v, ok
	}
}

// httpResolver builds the resolver for a .http file (or a quick request
// when file is ""): its variables, the env files up to the workspace
// root and the selected environment. env "" uses the selected one;
// "-" means none.
func httpResolver(file string, fileVars []httpclient.Var, env string) *httpclient.Resolver {
	dir, base := aiWorkspaceRoot(), ""
	if file != "" {
		dir = filepath.Dir(file)
		base = dir
	}
	root := httpRootFor(dir)
	if env == "" {
		env = httpSelectedEnv(root)
	} else if env == "-" {
		env = ""
	}
	globals, err := httpclient.LoadGlobals(httpGlobalsPath())
	if err != nil {
		recordError("http globals: " + err.Error())
	}
	return &httpclient.Resolver{
		Globals:  globals,
		FileVars: fileVars,
		Env:      env,
		Envs:     httpclient.LoadEnvs(dir, root),
		Secret:   httpSecretLookup(),
		OnSecret: ai.RegisterSecret,
		BaseDir:  base,
		Session:  httpSession,
	}
}

// httpSession holds the variables `# @pre/@post set` wrote; it lives
// until the app quits or HTTP: Clear Script Variables (API-8).
var httpSession = httpclient.NewSession()

// ── sending ─────────────────────────────────────────────────────────────

// httpEvent is one step of a send: progress or the answer.
type httpEvent struct {
	progress int64
	live     []byte // a streamed answer's newest text (httpLiveTail)
	resp     *httpclient.Response
	body     *httpBody // resp laid out for the page (nil: binary or empty)
	tests    []httpclient.AssertResult
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

// httpUserTLS applies the request's TLS directives (`# @insecure`,
// `# @ca-cert`, `# @client-cert`, `# @client-key`) to the user's own
// send; paths are read from the .http file's folder. AI sends never
// call it.
func httpUserTLS(run *httpRun, hooks *httpclient.Hooks) {
	dir := ""
	if run.file != "" {
		dir = filepath.Dir(run.file)
	}
	httpclient.ApplyTLSDirectives(hooks, run.rr.Request.Directives, dir)
}

// startHTTPRun sends run as the user's request: the lens and the status
// chip say "sending", the page opens on the answer.
func (m *Model) startHTTPRun(run *httpRun, hooks httpclient.Hooks) tea.Cmd {
	if httpclient.IsWebSocket(run.rr.Request) {
		return m.httpStartWS(run, hooks) // API-17
	}
	if cmd, wait := m.httpOAuthGate(run, hooks); wait {
		return cmd
	}
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
	httpUserTLS(run, &hooks)
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
	feed := &httpLiveFeed{ch: ch}
	hooks.Live = feed.push
	snap := *run // the goroutine never touches run itself
	crash.Go("the HTTP client", func() {
		resp := client.Send(ctx, snap.rr, hooks)
		feed.stop() // nothing is sent on ch after this
		snap.resp = resp
		// History and the page layout (pretty print, lines) side by
		// side, both off the UI loop.
		recorded := make(chan struct{})
		crash.Go("recording HTTP history", func() {
			defer close(recorded) // also after a panic, so the answer still shows
			httpRecordHistory(&snap)
		})
		var body *httpBody
		if !resp.IsBinary && len(resp.Body) > 0 {
			body = layoutHTTPBody(resp)
		}
		tests := httpRunTests(snap.rr, resp)
		<-recorded
		ch <- httpEvent{resp: resp, body: body, tests: tests}
		close(ch)
	})
	m.httpRefreshLens(run.file)
	m.httpShowRun(run, true)
	return waitHTTPEvent(gen, run, ch)
}

// handleHTTPMsg handles the HTTP client's messages. ok=false: not ours.
func (m *Model) handleHTTPMsg(msg tea.Msg) (tea.Cmd, bool) {
	if cmd, ok := m.handleHTTPOAuthMsg(msg); ok {
		return cmd, true
	}
	switch msg := msg.(type) {
	case httpEventMsg:
		s := m.httpS()
		if msg.gen != s.gen || s.running != msg.run {
			return nil, true
		}
		if msg.ev.resp == nil {
			msg.run.progress = msg.ev.progress
			next := waitHTTPEvent(msg.gen, msg.run, msg.ch)
			if msg.ev.live != nil {
				msg.run.live = msg.ev.live
				return tea.Batch(m.httpShowLive(), next), true
			}
			m.httpUpdateHead()
			return next, true
		}
		msg.run.resp, msg.run.body, msg.run.live = msg.ev.resp, msg.ev.body, nil
		msg.run.tests = msg.ev.tests
		return m.httpFinish(msg.run), true
	case httpAppendMsg:
		return m.httpPageAppend(msg), true
	case httpWSEventMsg:
		return m.httpWSHandle(msg), true
	case httpWSSentMsg:
		return m.httpWSSent(msg), true
	case httpOpenAPIFetchedMsg:
		return m.httpOpenAPIFetched(msg), true
	case httpMQTTDoneMsg:
		return m.onHTTPMQTTDone(msg), true
	case httpSIODoneMsg:
		return m.onHTTPSIODone(msg), true
	case httpGQLSchemaMsg:
		return m.onHTTPGQLSchema(msg), true
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
	if run.resp.ErrKind == httpclient.ErrKindCancelled && len(run.resp.Body) == 0 && s.shown != nil && s.shown != run && s.shown.resp != nil {
		// Keep the previous body; only row 2 says cancelled.
		prev := *s.shown
		r := *s.shown.resp
		r.Err, r.ErrKind = run.resp.Err, run.resp.ErrKind
		prev.resp = &r
		s.remember(run)
		m.httpRecordLens(run)
		cmd := m.httpShowRun(&prev, false)
		m.httpRefreshLens(run.file)
		if next := m.httpChainContinue(run); next != nil {
			return tea.Batch(cmd, next)
		}
		return cmd
	}
	s.remember(run)
	m.httpRecordLens(run)
	// The page first: it is what the user waits for. The lens redraw
	// (three editor calls) comes after it (FIX-48).
	cmd := m.httpShowRun(run, false)
	m.httpRefreshLens(run.file)
	if next := m.httpChainContinue(run); next != nil {
		return tea.Batch(cmd, next)
	}
	return cmd
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
	if t := httpTestsShort(run.tests); t != "" {
		l.text += " · " + t
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
	// API-8 checks.
	httpGlyphCheckOK   = theme.GlyphOK
	httpGlyphCheckFail = theme.GlyphFail
)

// API-16: a streamed answer is shown while it comes, at most
// httpLiveEvery apart, with its newest httpLiveMax bytes.
const (
	httpLiveEvery = 100 * time.Millisecond
	httpLiveMax   = 64 << 10
)

// httpLiveTail copies the newest httpLiveMax bytes of body, from the
// start of a line (the client reuses body after the call).
func httpLiveTail(body []byte) []byte {
	if len(body) > httpLiveMax {
		body = body[len(body)-httpLiveMax:]
		if i := bytes.IndexByte(body, '\n'); i >= 0 {
			body = body[i+1:]
		}
	}
	return append([]byte(nil), body...)
}

// httpLiveFeed sends a stream's newest text to the UI loop: right away
// when the last send is httpLiveEvery old, else once that time is up
// (so the last chunk before a pause is never left out).
type httpLiveFeed struct {
	ch      chan<- httpEvent
	mu      sync.Mutex
	last    time.Time
	pending []byte
	pendN   int64 // body bytes when pending was taken
	timer   *time.Timer
	done    bool
}

func (f *httpLiveFeed) push(body []byte) {
	tail, n := httpLiveTail(body), int64(len(body))
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.done {
		return
	}
	if wait := httpLiveEvery - time.Since(f.last); wait > 0 {
		f.pending, f.pendN = tail, n
		if f.timer == nil {
			f.timer = time.AfterFunc(wait, f.flush)
		}
		return
	}
	f.sendLocked(tail, n)
}

func (f *httpLiveFeed) flush() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.timer = nil
	if f.done || f.pending == nil {
		return
	}
	f.sendLocked(f.pending, f.pendN)
}

func (f *httpLiveFeed) sendLocked(tail []byte, n int64) {
	f.pending, f.last = nil, time.Now()
	select {
	case f.ch <- httpEvent{progress: n, live: tail}:
	default: // the UI is busy; a newer text follows
	}
}

// stop ends the feed before the channel closes.
func (f *httpLiveFeed) stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.done = true
	if f.timer != nil {
		f.timer.Stop()
	}
}
