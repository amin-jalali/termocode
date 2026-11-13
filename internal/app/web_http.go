package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The HTTP client from the page (WEBP-HTTP). The laptop reads the
// workspace's .http files and environments and sends every request with
// its own client, as its HTTP client does; the page only names what to
// send. What goes back is masked: request headers as written with
// secret values hidden, response headers redacted, bodies through the
// AI's redaction (which knows every private or ${secret:} value the
// resolver used), environment values of private files or secret names
// hidden. A request to this machine runs at once; any other host waits
// for a laptop card the first time per host in this session (a private
// network address every time). A redirect to a host the web was not
// allowed stops the send; a public host may not connect to a private
// address (DNS rebinding).

func init() {
	webOps[webproto.OpHTTPFiles] = (*Model).webHTTPFiles
	webOps[webproto.OpHTTPSend] = (*Model).webHTTPSend
	webOps[webproto.OpHTTPCancel] = (*Model).webHTTPCancel
	webOps[webproto.OpHTTPResponse] = (*Model).webHTTPResponse
	webOps[webproto.OpHTTPHistory] = (*Model).webHTTPHistory
	webOps[webproto.OpHTTPEnvs] = (*Model).webHTTPEnvs
	webOps[webproto.OpHTTPEnvSelect] = (*Model).webHTTPEnvSelect
	webOps[webproto.OpHTTPEnvFile] = (*Model).webHTTPEnvFile
	webOps[webproto.OpHTTPSaved] = (*Model).webHTTPSaved
	webOps[webproto.OpHTTPSave] = (*Model).webHTTPSave
	webOps[webproto.OpHTTPCurl] = (*Model).webHTTPCurl
	webOps[webproto.OpHTTPImportCurl] = (*Model).webHTTPImportCurl
	webOps[webproto.OpHTTPNewFile] = (*Model).webHTTPNewFile
	webOps[webproto.OpHTTPSaveBody] = (*Model).webHTTPSaveBody
}

// webHTTPDo sends one request (a test hook).
var webHTTPDo = webHTTPSendReal

func webHTTPSendReal(ctx context.Context, c *httpclient.Client, rr httpclient.Resolved, h httpclient.Hooks) *httpclient.Response {
	return c.Send(ctx, rr, h)
}

var (
	errWebHTTPNoRun  = webErr(webproto.CodeNotFound, "No response yet. Send a request first.")
	errWebHTTPNoReq  = webErr(webproto.CodeNotFound, "That request is not in the file.")
	errWebHTTPNoFile = webErr(webproto.CodeBadRequest, "That is not a .http or .rest file.")
)

// webHTTPRoot is the workspace root (a local one: the HTTP client reads
// and writes files there).
func (m *Model) webHTTPRoot(call *webCall) (string, bool) {
	if !m.webFileOpsLocal(call) {
		return "", false
	}
	root, err := m.rootDir()
	if err != nil {
		call.fail(webproto.CodeNotFound, "No workspace is open.")
		return "", false
	}
	return root, true
}

// webHTTPLater runs fn off the UI thread and answers what it returns.
func webHTTPLater(call *webCall, fn func() (any, error)) tea.Cmd {
	call.later()
	return func() tea.Msg {
		out, err := fn()
		if err != nil {
			webFailErr(call, err, "That did not work.")
			return nil
		}
		call.reply(out)
		return nil
	}
}

// webHTTPRelOf is a request file as the page names it.
func (m *Model) webHTTPRelOf(file string) string {
	if file == "" {
		return ""
	}
	if rel, ok := m.webRel(file); ok {
		return rel
	}
	return filepath.Base(file)
}

// webHTTPMask is a value for the page: secrets the resolver used and
// what looks like one hidden.
func webHTTPMask(s string) string { return ai.Redact(strings.ToValidUTF8(s, "?")) }

func webHTTPHeaders(hs []httpclient.Header) []webproto.HTTPHeader {
	out := make([]webproto.HTTPHeader, 0, len(hs))
	for _, h := range hs {
		out = append(out, webproto.HTTPHeader{Name: h.Name, Value: webHTTPMask(httpclient.RedactHeaderValue(h.Name, h.Value))})
	}
	return out
}

func webHTTPItem(i int, r httpclient.Request) webproto.HTTPReqItem {
	return webproto.HTTPReqItem{Index: i + 1, Name: r.Name, Method: r.Method, URL: webHTTPMask(httpclient.RedactURL(r.URL)), Line: r.Line}
}

// webHTTPResolver resolves against the workspace's environment files and
// the selected environment (env "": the selected one, "-": none).
func webHTTPResolver(root, file string, vars []httpclient.Var, env string) *httpclient.Resolver {
	dir, base := root, ""
	if file != "" {
		dir = filepath.Dir(file)
		base = dir
	}
	if env == "" {
		env = httpSelectedEnv(root)
	} else if env == "-" {
		env = ""
	}
	return &httpclient.Resolver{FileVars: vars, Env: env, Envs: httpclient.LoadEnvs(dir, root), Secret: httpSecretLookup(), OnSecret: ai.RegisterSecret, BaseDir: base}
}

// webHTTPFileReq is request index (1-based) of a workspace .http file,
// with the file's variables (the open buffer's text when it has one).
func (m *Model) webHTTPFileReq(rel string, index int) (string, httpclient.File, httpclient.Request, error) {
	abs, err := m.webResolve(rel)
	if err != nil {
		return "", httpclient.File{}, httpclient.Request{}, err
	}
	if !isHTTPPath(abs) {
		return "", httpclient.File{}, httpclient.Request{}, errWebHTTPNoFile
	}
	f, ok := m.httpReadFile(abs)
	if !ok {
		return "", f, httpclient.Request{}, webErr(webproto.CodeNotFound, "That file cannot be read.")
	}
	if index < 1 || index > len(f.Requests) {
		return "", f, httpclient.Request{}, errWebHTTPNoReq
	}
	return abs, f, f.Requests[index-1], nil
}

// ── reads ───────────────────────────────────────────────────────────────

func (m *Model) webHTTPFiles(call *webCall) tea.Cmd {
	root, ok := m.webHTTPRoot(call)
	if !ok {
		return nil
	}
	rel := m.webHTTPRelOf
	return webHTTPLater(call, func() (any, error) {
		out := webproto.HTTPFiles{Files: []webproto.HTTPFile{}, Env: httpSelectedEnv(root), Envs: httpclient.LoadEnvs(root, root).Names()}
		if out.Envs == nil {
			out.Envs = []string{}
		}
		files := httpWorkspaceFiles(root, webproto.MaxHTTPFiles+1)
		if len(files) > webproto.MaxHTTPFiles {
			out.More = len(files) - webproto.MaxHTTPFiles
			files = files[:webproto.MaxHTTPFiles]
		}
		for _, file := range files {
			hf := webproto.HTTPFile{Path: rel(file), Requests: []webproto.HTTPReqItem{}}
			data, err := os.ReadFile(file)
			if err != nil {
				hf.Err = "This file cannot be read."
			}
			for i, r := range httpclient.Parse(string(data)).Requests {
				if i == webproto.MaxHTTPRequests {
					break
				}
				hf.Requests = append(hf.Requests, webHTTPItem(i, r))
			}
			out.Files = append(out.Files, hf)
		}
		return out, nil
	})
}

func (m *Model) webHTTPResponse(call *webCall) tea.Cmd {
	a := call.args.(*webproto.HTTPRunArgs)
	s := m.httpS()
	run, running := s.findRun(a.Run), false
	if w := s.webRun; w != nil && (a.Run == 0 || a.Run == w.id) {
		run, running = w, true
	}
	if run == nil {
		webFailErr(call, errWebHTTPNoRun, "")
		return nil
	}
	out := webproto.HTTPResponse{Run: run.id, Running: running, By: run.by, Label: webHTTPMask(run.label), Method: run.rr.Method,
		URL: webHTTPMask(httpclient.RedactURL(run.rr.Template.URL)), File: m.webHTTPRelOf(run.file), Env: run.env,
		ReqHeaders: webHTTPHeaders(httpclient.RedactTemplateHeaders(run.rr.Template.Headers)), Headers: []webproto.HTTPHeader{}}
	resp := run.resp
	if running || resp == nil {
		call.reply(out)
		return nil
	}
	return webHTTPLater(call, func() (any, error) {
		out.Status, out.StatusText, out.Proto = resp.Status, resp.StatusText, resp.Proto
		out.ElapsedMS, out.Size, out.ContentType = resp.Elapsed.Milliseconds(), resp.Size, resp.ContentType
		out.Headers = webHTTPHeaders(httpclient.RedactHeaders(resp.Headers))
		out.Err = webHTTPMask(resp.Err)
		out.Truncated = resp.Truncated
		for _, h := range resp.Hops {
			out.Hops = append(out.Hops, fmt.Sprintf("%d %s", h.Status, webHTTPMask(httpclient.RedactURL(h.URL))))
		}
		if resp.IsBinary {
			out.Binary = true
			return out, nil
		}
		text := httpclient.DecodeText(resp.Body, resp.Charset)
		if len(text) > webproto.MaxHTTPShown {
			cut := webproto.MaxHTTPShown
			for cut > 0 && !utf8.RuneStart(text[cut]) {
				cut--
			}
			text, out.Truncated = text[:cut], true
		}
		out.Body = webHTTPMask(text)
		return out, nil
	})
}

func (m *Model) webHTTPHistory(call *webCall) tea.Cmd {
	rel := m.webHTTPRelOf
	return webHTTPLater(call, func() (any, error) {
		list, err := httpHistoryStore().List()
		if err != nil {
			return nil, webErr(webproto.CodeInternal, "The history cannot be read.")
		}
		out := webproto.HTTPHistory{Entries: []webproto.HTTPHistItem{}}
		for i, e := range list {
			if i == webproto.MaxHTTPHistory {
				break
			}
			file := e.File
			if filepath.IsAbs(file) {
				file = rel(file)
			}
			out.Entries = append(out.Entries, webproto.HTTPHistItem{ID: e.ID, At: e.Time.UnixMilli(), Method: e.Method, URL: webHTTPMask(httpclient.RedactURL(e.URL)),
				Status: e.Status, Err: webHTTPMask(e.Err), ElapsedMS: e.ElapsedMS, Size: e.Size, File: file, Name: webHTTPMask(e.Name), Env: e.Env, By: e.By})
		}
		return out, nil
	})
}

func (m *Model) webHTTPEnvs(call *webCall) tea.Cmd {
	root, ok := m.webHTTPRoot(call)
	if !ok {
		return nil
	}
	rel := m.webHTTPRelOf
	return webHTTPLater(call, func() (any, error) {
		e := httpclient.LoadEnvs(root, root)
		out := webproto.HTTPEnvs{Selected: httpSelectedEnv(root), Files: []string{}, Envs: []webproto.HTTPEnv{}}
		for _, f := range e.Files {
			out.Files = append(out.Files, rel(f))
		}
		for _, n := range e.Names() {
			env := webproto.HTTPEnv{Name: n, Vars: []webproto.HTTPVar{}}
			for _, v := range e.VarNames(n) {
				val, priv, _ := e.Lookup(n, v)
				if priv || httpSecretName(v) {
					env.Vars = append(env.Vars, webproto.HTTPVar{Name: v, Hidden: true})
					continue
				}
				env.Vars = append(env.Vars, webproto.HTTPVar{Name: v, Value: webHTTPMask(val)})
			}
			out.Envs = append(out.Envs, env)
		}
		return out, nil
	})
}

func (m *Model) webHTTPSaved(call *webCall) tea.Cmd {
	return webHTTPLater(call, func() (any, error) {
		b, _ := os.ReadFile(httpSavedPath())
		out := webproto.HTTPSaved{Requests: []webproto.HTTPReqItem{}}
		for i, r := range httpclient.Parse(string(b)).Requests {
			if i == webproto.MaxHTTPRequests {
				break
			}
			out.Requests = append(out.Requests, webHTTPItem(i, r))
		}
		return out, nil
	})
}

// webHTTPCurl answers a run's request, or a file's, as one curl line
// with secret values as their {{template}}.
func (m *Model) webHTTPCurl(call *webCall) tea.Cmd {
	a := call.args.(*webproto.HTTPCurlArgs)
	var rr httpclient.Resolved
	if a.Path != "" {
		root, ok := m.webHTTPRoot(call)
		if !ok {
			return nil
		}
		abs, f, req, err := m.webHTTPFileReq(a.Path, a.Index)
		if err != nil {
			webFailErr(call, err, "")
			return nil
		}
		rr, _ = webHTTPResolver(root, abs, f.Vars, "").Resolve(req)
	} else {
		run := m.httpS().findRun(a.Run)
		if run == nil {
			webFailErr(call, errWebHTTPNoRun, "")
			return nil
		}
		rr = run.rr
	}
	call.reply(webproto.HTTPCurl{Curl: webHTTPMask(httpclient.ToCurl(rr, false))})
	return nil
}

// ── sending ─────────────────────────────────────────────────────────────

// webHTTPGate says whether a request to rawURL waits for the laptop:
// this machine never does, a private network address always does, any
// other host until the laptop allowed it in this session.
func (s *httpState) webHTTPGate(rawURL string) (host string, ask, remember bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", true, false
	}
	host = strings.ToLower(u.Host)
	switch httpclient.ClassifyHost(u.Hostname()) {
	case httpclient.HostLoopback:
		return host, false, false
	case httpclient.HostPrivate:
		return host, true, false
	}
	return host, !s.webHosts[host], true
}

// webHTTPHooks are the rules of one web send: a public host may not
// connect to a private address; a redirect may go to the same host, this
// machine or a host the web was allowed, else the send stops there.
func (s *httpState) webHTTPHooks(rawURL string) httpclient.Hooks {
	u, _ := url.Parse(rawURL)
	key, cls := "", httpclient.HostPublic
	if u != nil {
		key, cls = strings.ToLower(u.Host), httpclient.ClassifyHost(u.Hostname())
	}
	allowed := map[string]bool{}
	for h := range s.webHosts {
		allowed[h] = true
	}
	var h httpclient.Hooks
	if cls == httpclient.HostPublic {
		h.AllowIP = func(ip net.IP) bool { return !httpclient.IsPrivateIP(ip) }
	}
	h.CheckHop = func(next string) error {
		nu, err := url.Parse(next)
		if err != nil {
			return errors.New("redirect to a bad URL")
		}
		k := strings.ToLower(nu.Host)
		if k == key || allowed[k] || httpclient.ClassifyHost(nu.Hostname()) == httpclient.HostLoopback {
			return nil
		}
		return fmt.Errorf("stopped at a redirect to %s: send to it from the web first so the laptop can allow it", nu.Host)
	}
	return h
}

// webHTTPSource reads the request the page named: its .http file (abs,
// "" for none), the file's variables and the template.
func (m *Model) webHTTPSource(root string, a *webproto.HTTPSendArgs) (string, []httpclient.Var, httpclient.Request, error) {
	switch {
	case a.Path != "":
		abs, f, req, err := m.webHTTPFileReq(a.Path, a.Index)
		return abs, f.Vars, req, err
	case a.History != "":
		e, ok := httpHistoryStore().Get(a.History)
		if !ok {
			return "", nil, httpclient.Request{}, webErr(webproto.CodeNotFound, "That history entry is gone.")
		}
		tmpl := e.Request()
		if e.File == "" {
			return "", nil, tmpl, nil
		}
		file := e.File
		if !filepath.IsAbs(file) {
			file = filepath.Join(root, filepath.FromSlash(file))
		}
		if _, inside := m.webRel(file); !inside {
			return "", nil, tmpl, nil
		}
		if f, ok := m.httpReadFile(file); ok {
			for _, r := range f.Requests {
				if r.Label() == e.Name {
					return file, f.Vars, r, nil
				}
			}
			return file, f.Vars, tmpl, nil
		}
		return "", nil, tmpl, nil
	case a.Saved != 0:
		b, _ := os.ReadFile(httpSavedPath())
		reqs := httpclient.Parse(string(b)).Requests
		if a.Saved > len(reqs) {
			return "", nil, httpclient.Request{}, webErr(webproto.CodeNotFound, "That saved request is gone.")
		}
		return "", nil, reqs[a.Saved-1], nil
	}
	method := strings.ToUpper(a.Method)
	if method == "" {
		method = "GET"
	}
	req := httpclient.Request{Method: method, URL: a.URL}
	for _, h := range a.Headers {
		req.Headers = append(req.Headers, httpclient.Header{Name: h.Name, Value: h.Value})
	}
	if a.Body != "" {
		body := a.Body
		req.Body = &body
	}
	if len(a.Proto)+len(a.ProtoDirs) > 0 {
		if !isGRPCRequest(req) {
			return "", nil, req, webErr(webproto.CodeBadRequest, "@proto files belong to a GRPC request.")
		}
		dirs := map[string]string{}
		for key, list := range map[string][]string{"proto": a.Proto, "proto-path": a.ProtoDirs} {
			for _, rel := range list {
				if err := m.webHTTPProtoCheck(root, rel, key == "proto-path"); err != nil {
					return "", nil, req, err
				}
			}
			if len(list) > 0 {
				dirs[key] = strings.Join(list, " ")
			}
		}
		req.Directives = dirs
	}
	return "", nil, req, nil
}

// webHTTPProtoCheck: a typed GRPC request's @proto file (or @proto-path
// folder) is read from the workspace only: it exists, stays inside it
// after symlinks, and is no secret file.
func (m *Model) webHTTPProtoCheck(root, rel string, dir bool) error {
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if webSecretFile(abs) {
		return webErrSecret()
	}
	real, err := filepath.EvalSymlinks(abs)
	rootReal, rerr := filepath.EvalSymlinks(root)
	if err != nil || rerr != nil {
		return webErr(webproto.CodeBadRequest, "There is no such @proto file or folder: "+rel)
	}
	if !webInside(filepath.Clean(rootReal), filepath.Clean(real)) || webSecretFile(real) {
		return webErr(webproto.CodeForbidden, "An @proto file or folder must be inside the workspace.")
	}
	if st, err := os.Stat(real); err != nil || st.IsDir() != dir {
		return webErr(webproto.CodeBadRequest, "There is no such @proto file or folder: "+rel)
	}
	return nil
}

func (m *Model) webHTTPSend(call *webCall) tea.Cmd {
	a := call.args.(*webproto.HTTPSendArgs)
	root, ok := m.webHTTPRoot(call)
	if !ok {
		return nil
	}
	s := m.httpS()
	if s.webRun != nil {
		call.fail(webproto.CodeBusy, "A request from the web is still running.")
		return nil
	}
	if a.Env != "" && a.Env != "-" && !contains(httpclient.LoadEnvs(root, root).Names(), a.Env) {
		call.fail(webproto.CodeNotFound, "There is no such environment.")
		return nil
	}
	file, vars, req, err := m.webHTTPSource(root, a)
	if err != nil {
		webFailErr(call, err, "")
		return nil
	}
	rr, un := webHTTPResolver(root, file, vars, a.Env).Resolve(req)
	if len(un) > 0 {
		seen := map[string]bool{}
		var names []string
		for _, u := range un {
			if !seen[u.Name] {
				seen[u.Name] = true
				names = append(names, "{{"+u.Name+"}}")
			}
		}
		call.fail(webproto.CodeBadRequest, "Unknown variables: "+strings.Join(names, ", ")+". Define them in an environment file or with @name = value.")
		return nil
	}
	gateURL := rr.URL
	if isGRPCRequest(rr.Request) {
		if err := httpGRPCValidateURL(rr.URL); err != nil {
			call.fail(webproto.CodeBadRequest, webHTTPMask(err.Error()))
			return nil
		}
		gateURL = webHTTPGRPCGateURL(rr.URL)
	} else if u, err := url.Parse(rr.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		call.fail(webproto.CodeBadRequest, "Only http:// and https:// addresses (or a GRPC request) can be sent.")
		return nil
	}
	run := &httpRun{rr: rr, file: file, line: req.Line, label: req.Label(), env: rr.Env}
	host, ask, remember := s.webHTTPGate(gateURL)
	if !ask {
		cmd := m.webHTTPStart(run)
		call.reply(webproto.HTTPSent{Run: run.id})
		return cmd
	}
	shown := rr.Method + " " + webHTTPMask(httpclient.RedactURL(rr.Template.URL))
	body := "The web asks the laptop to send:\n\n" + shown + "\n\nHost: " + host + "."
	if remember {
		body += " After you allow it, the web can send to this host until termocode quits."
	} else {
		body += " This is a private network address: the laptop asks each time."
	}
	ask0 := &webAsk{summary: shown, title: "Send a request from the web?", action: "Send", body: body}
	ask0.run = func(m *Model) tea.Cmd {
		s := m.httpS()
		if remember {
			s.webHosts[host] = true
		}
		if s.webRun != nil {
			return m.pushToastDetail(toast.Warn, "Not sent", "A request from the web is still running.")
		}
		return m.webHTTPStart(run)
	}
	if id, ok := m.webAskQueue(call, ask0); ok {
		call.reply(webproto.HTTPSent{Card: id, Host: host})
	}
	return nil
}

// webHTTPStart sends run as the web's request (one at a time) with the
// laptop's client, off the UI thread; the answer goes to the ring and
// the history like the laptop's own.
func (m *Model) webHTTPStart(run *httpRun) tea.Cmd {
	s := m.httpS()
	set := loadHTTPSettings()
	client := s.httpClient(set)
	s.nextID++
	run.id, run.by, run.at = s.nextID, "web", time.Now()
	hooks := s.webHTTPHooks(run.rr.URL)
	hooks.Insecure = set.tlsInsecure()
	ctx, cancel := context.WithCancel(context.Background())
	s.webRun, s.webCancel = run, cancel
	snap := *run
	grpcFile := snap.file
	if grpcFile == "" { // typed on the page: relative @proto paths are the workspace's
		if root, err := m.rootDir(); err == nil {
			grpcFile = filepath.Join(root, "ad-hoc.http")
		}
	}
	return func() tea.Msg {
		defer cancel()
		var resp *httpclient.Response
		if isGRPCRequest(snap.rr.Request) {
			resp = webHTTPGRPCDo(ctx, snap.rr, grpcFile, hooks.Insecure)
		} else {
			resp = webHTTPDo(ctx, client, snap.rr, hooks)
		}
		if resp == nil {
			resp = &httpclient.Response{Err: "no answer"}
		}
		snap.resp = resp
		httpRecordHistory(&snap)
		return applyMsg(func(m *Model) tea.Cmd {
			s := m.httpS()
			if s.webRun == run {
				s.webRun, s.webCancel = nil, nil
			}
			run.resp = resp
			s.remember(run)
			m.httpRecordLens(run)
			m.httpRefreshLens(run.file)
			return nil
		})
	}
}

func (m *Model) webHTTPCancel(call *webCall) tea.Cmd {
	s := m.httpS()
	if s.webRun == nil || s.webCancel == nil {
		call.fail(webproto.CodeNotFound, "No request from the web is running.")
		return nil
	}
	s.webCancel()
	call.reply(nil)
	return nil
}

// ── changes ─────────────────────────────────────────────────────────────

func (m *Model) webHTTPEnvSelect(call *webCall) tea.Cmd {
	a := call.args.(*webproto.HTTPEnvArgs)
	root, ok := m.webHTTPRoot(call)
	if !ok {
		return nil
	}
	if a.Name != "" && !contains(httpclient.LoadEnvs(root, root).Names(), a.Name) {
		call.fail(webproto.CodeNotFound, "There is no such environment.")
		return nil
	}
	if err := httpSetSelectedEnv(root, a.Name); err != nil {
		webFailErr(call, err, "The environment was not changed.")
		return nil
	}
	m.httpRefreshLens(m.httpCurrentFile())
	call.reply(nil)
	return nil
}

// webHTTPEnvFile is the workspace's environment file, made from the
// template when there is none. The private one stays on the laptop.
func (m *Model) webHTTPEnvFile(call *webCall) tea.Cmd {
	root, ok := m.webHTTPRoot(call)
	if !ok {
		return nil
	}
	p := filepath.Join(root, httpclient.EnvFile)
	if _, err := os.Stat(p); err != nil {
		if err := os.WriteFile(p, []byte(httpEnvTemplate), 0o644); err != nil {
			webFailErr(call, err, "Could not create it:")
			return nil
		}
		m.explorer.Reload()
	}
	call.reply(webproto.HTTPPath{Path: httpclient.EnvFile})
	return nil
}

func (m *Model) webHTTPSave(call *webCall) tea.Cmd {
	a := call.args.(*webproto.HTTPSaveArgs)
	var req httpclient.Request
	if a.Path != "" {
		if _, ok := m.webHTTPRoot(call); !ok {
			return nil
		}
		_, _, r, err := m.webHTTPFileReq(a.Path, a.Index)
		if err != nil {
			webFailErr(call, err, "")
			return nil
		}
		req = r
	} else {
		run := m.httpS().findRun(a.Run)
		if run == nil {
			webFailErr(call, errWebHTTPNoRun, "")
			return nil
		}
		req = run.rr.Template
	}
	req.Name = strings.TrimSpace(a.Name)
	req.Headers = httpclient.RedactTemplateHeaders(req.Headers)
	req.Directives = nil
	return webHTTPLater(call, func() (any, error) {
		p := httpSavedPath()
		b, _ := os.ReadFile(p)
		f := httpclient.Parse(string(b))
		f.Requests = append(f.Requests, req)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, []byte(httpclient.Serialize(f.Requests)), 0o600); err != nil {
			return nil, err
		}
		return nil, nil
	})
}

// webHTTPTarget is a workspace .http file to write: an existing one, or
// a new one whose folder exists.
func (m *Model) webHTTPTarget(rel string) (string, error) {
	rel = webCleanRel(rel)
	if !isHTTPPath(rel) {
		return "", errWebHTTPNoFile
	}
	abs, err := m.webResolve(rel)
	var we *webError
	if errors.As(err, &we) && we.code == webproto.CodeNotFound {
		return m.webResolveNew(rel)
	}
	return abs, err
}

func (m *Model) webHTTPImportCurl(call *webCall) tea.Cmd {
	a := call.args.(*webproto.HTTPImportArgs)
	if _, ok := m.webHTTPRoot(call); !ok {
		return nil
	}
	text := strings.TrimSpace(a.Curl)
	if text != "curl" && !strings.HasPrefix(text, "curl ") {
		call.fail(webproto.CodeBadRequest, "Paste a curl command.")
		return nil
	}
	req, err := httpclient.FromCurl(text)
	if err != nil {
		call.fail(webproto.CodeBadRequest, "That curl command cannot be read: "+webHTTPMask(err.Error()))
		return nil
	}
	rel := a.Path
	if rel == "" {
		rel = "scratch.http"
	}
	abs, err := m.webHTTPTarget(rel)
	if err != nil {
		webFailErr(call, err, "")
		return nil
	}
	old, _ := os.ReadFile(abs)
	doc := string(old)
	if strings.TrimSpace(doc) != "" {
		if !strings.HasSuffix(doc, "\n") {
			doc += "\n"
		}
		doc += "\n###\n"
	}
	line := strings.Count(doc, "\n") + 1
	doc += httpclient.Serialize([]httpclient.Request{req})
	if err := os.WriteFile(abs, []byte(doc), 0o644); err != nil {
		webFailErr(call, err, "Could not write it:")
		return nil
	}
	m.explorer.Reload()
	call.reply(webproto.HTTPPath{Path: m.webHTTPRelOf(abs), Line: line})
	return nil
}

func (m *Model) webHTTPNewFile(call *webCall) tea.Cmd {
	a := call.args.(*webproto.HTTPNewFileArgs)
	if _, ok := m.webHTTPRoot(call); !ok {
		return nil
	}
	rel := webCleanRel(a.Path)
	if !isHTTPPath(rel) {
		webFailErr(call, errWebHTTPNoFile, "")
		return nil
	}
	abs, err := m.webResolveNew(rel)
	if err != nil {
		webFailErr(call, err, "")
		return nil
	}
	if err := os.WriteFile(abs, []byte(httpNewFileTemplate), 0o644); err != nil {
		webFailErr(call, err, "Could not create it:")
		return nil
	}
	m.explorer.Reload()
	call.reply(webproto.HTTPPath{Path: m.webHTTPRelOf(abs)})
	return nil
}

func (m *Model) webHTTPSaveBody(call *webCall) tea.Cmd {
	a := call.args.(*webproto.HTTPSaveBodyArgs)
	if _, ok := m.webHTTPRoot(call); !ok {
		return nil
	}
	abs, err := m.webResolveNew(webCleanRel(a.Path))
	if err != nil {
		webFailErr(call, err, "")
		return nil
	}
	run := m.httpS().findRun(a.Run)
	if run == nil || run.resp == nil || len(run.resp.Body) == 0 {
		call.fail(webproto.CodeNotFound, "That response has no body.")
		return nil
	}
	body := run.resp.Body
	if err := os.WriteFile(abs, body, 0o644); err != nil {
		webFailErr(call, err, "Could not write it:")
		return nil
	}
	m.explorer.Reload()
	call.reply(webproto.HTTPPath{Path: path.Clean(m.webHTTPRelOf(abs)), Size: int64(len(body))})
	return nil
}
