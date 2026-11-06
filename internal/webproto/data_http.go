package webproto

import "strings"

// HTTP client (WEBP-HTTP): the laptop's .http requests, environments,
// history and saved requests, as its HTTP client has them. The laptop
// sends every request; the page only names it. What comes back is
// masked by the laptop: secret header values, ${secret:} and private
// environment values never reach the page. A request to a host that is
// not this machine waits for a laptop card the first time per host
// (http.send answers with the card's id then).

// HTTP op names.
const (
	OpHTTPFiles      = "http.files"
	OpHTTPSend       = "http.send"
	OpHTTPCancel     = "http.cancel"
	OpHTTPResponse   = "http.response"
	OpHTTPHistory    = "http.history"
	OpHTTPEnvs       = "http.envs"
	OpHTTPEnvSelect  = "http.env_select"
	OpHTTPEnvFile    = "http.env_file"
	OpHTTPSaved      = "http.saved"
	OpHTTPSave       = "http.save"
	OpHTTPCurl       = "http.curl"
	OpHTTPImportCurl = "http.import_curl"
	OpHTTPNewFile    = "http.new_file"
	OpHTTPSaveBody   = "http.save_body"
)

var opHTTPRows = []Policy{
	{Op: OpHTTPFiles, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpHTTPSend, New: newOf[HTTPSendArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: MaxHTTPBody + 64*kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpHTTPCancel, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpHTTPResponse, New: newOf[HTTPRunArgs](), Role: RoleViewer, Rate: perSec(10), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpHTTPHistory, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpHTTPEnvs, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpHTTPEnvSelect, New: newOf[HTTPEnvArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpHTTPEnvFile, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpHTTPSaved, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpHTTPSave, New: newOf[HTTPSaveArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpHTTPCurl, New: newOf[HTTPCurlArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 8 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpHTTPImportCurl, New: newOf[HTTPImportArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: MaxHTTPCurl + 8*kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpHTTPNewFile, New: newOf[HTTPNewFileArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpHTTPSaveBody, New: newOf[HTTPSaveBodyArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opHTTPRows...); return true }()

func init() {
	registerResult(OpHTTPFiles, HTTPFiles{})
	registerResult(OpHTTPSend, HTTPSent{})
	registerResult(OpHTTPResponse, HTTPResponse{})
	registerResult(OpHTTPHistory, HTTPHistory{})
	registerResult(OpHTTPEnvs, HTTPEnvs{})
	registerResult(OpHTTPEnvFile, HTTPPath{})
	registerResult(OpHTTPSaved, HTTPSaved{})
	registerResult(OpHTTPCurl, HTTPCurl{})
	registerResult(OpHTTPImportCurl, HTTPPath{})
	registerResult(OpHTTPNewFile, HTTPPath{})
	registerResult(OpHTTPSaveBody, HTTPPath{})
}

// HTTP limits.
const (
	MaxHTTPBody     = 512 << 10 // bytes of a request body from the page
	MaxHTTPURL      = 8 << 10
	MaxHTTPHeaders  = 50
	MaxHTTPHeader   = 8 << 10 // bytes of one header value
	MaxHTTPCurl     = 64 << 10
	MaxHTTPName     = 200
	MaxHTTPFiles    = 40
	MaxHTTPRequests = 100 // per file
	MaxHTTPHistory  = 100
	MaxHTTPShown    = 256 << 10 // bytes of a response body sent to the page
)

// HTTPHeader is one header line. A secret value comes masked ("••••").
type HTTPHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HTTPFiles answers http.files: the workspace's .http and .rest files
// with their requests (templates, {{variables}} kept), the selected
// environment and the environments' names.
type HTTPFiles struct {
	Files []HTTPFile `json:"files"`
	Env   string     `json:"env,omitempty"`
	Envs  []string   `json:"envs"`
	More  int        `json:"more,omitempty"`
}

// HTTPFile is one .http file.
type HTTPFile struct {
	Path     string        `json:"path"`
	Requests []HTTPReqItem `json:"requests"`
	Err      string        `json:"err,omitempty"`
}

// HTTPReqItem is one request of a file (Index is 1-based) or of the
// saved list; the URL keeps its {{variables}}, query secrets masked.
type HTTPReqItem struct {
	Index  int    `json:"index"`
	Name   string `json:"name,omitempty"`
	Method string `json:"method"`
	URL    string `json:"url"`
	Line   int    `json:"line,omitempty"`
}

// HTTPSendArgs names one request: Path and Index (a request of a .http
// file), History (an entry id), Saved (1-based) or Method, URL, Headers
// and Body (a request typed on the page). Env "" is the selected
// environment, "-" none.
type HTTPSendArgs struct {
	Path    string       `json:"path,omitempty"`
	Index   int          `json:"index,omitempty"`
	Method  string       `json:"method,omitempty"`
	URL     string       `json:"url,omitempty"`
	Headers []HTTPHeader `json:"headers,omitempty"`
	Body    string       `json:"body,omitempty"`
	Env     string       `json:"env,omitempty"`
	History string       `json:"history,omitempty"`
	Saved   int          `json:"saved,omitempty"`
}

// HTTPSent answers http.send: the run's number, or the laptop card's id
// when the host needs the laptop's yes first (the send starts then).
type HTTPSent struct {
	Run  int    `json:"run,omitempty"`
	Card string `json:"card,omitempty"`
	Host string `json:"host,omitempty"`
}

// HTTPRunArgs names a run (0: the newest).
type HTTPRunArgs struct {
	Run int `json:"run,omitempty"`
}

// HTTPResponse answers http.response: one run, the request as written
// (secrets masked) and, once done, the answer. Times in ms.
type HTTPResponse struct {
	Run         int          `json:"run"`
	Running     bool         `json:"running,omitempty"`
	By          string       `json:"by,omitempty"` // user, ai, web
	Label       string       `json:"label"`
	Method      string       `json:"method"`
	URL         string       `json:"url"`
	File        string       `json:"file,omitempty"`
	Env         string       `json:"env,omitempty"`
	ReqHeaders  []HTTPHeader `json:"req_headers"`
	Status      int          `json:"status,omitempty"`
	StatusText  string       `json:"status_text,omitempty"`
	Proto       string       `json:"proto,omitempty"`
	ElapsedMS   int64        `json:"elapsed_ms,omitempty"`
	Size        int64        `json:"size,omitempty"`
	ContentType string       `json:"content_type,omitempty"`
	Headers     []HTTPHeader `json:"headers"`
	Body        string       `json:"body,omitempty"`
	Binary      bool         `json:"binary,omitempty"`
	Truncated   bool         `json:"truncated,omitempty"`
	Hops        []string     `json:"hops,omitempty"`
	Err         string       `json:"err,omitempty"`
}

// HTTPHistory answers http.history, newest first.
type HTTPHistory struct {
	Entries []HTTPHistItem `json:"entries"`
}

// HTTPHistItem is one past request. At is Unix ms.
type HTTPHistItem struct {
	ID        string `json:"id"`
	At        int64  `json:"at"`
	Method    string `json:"method"`
	URL       string `json:"url"`
	Status    int    `json:"status,omitempty"`
	Err       string `json:"err,omitempty"`
	ElapsedMS int64  `json:"elapsed_ms,omitempty"`
	Size      int64  `json:"size,omitempty"`
	File      string `json:"file,omitempty"`
	Name      string `json:"name,omitempty"`
	Env       string `json:"env,omitempty"`
	By        string `json:"by,omitempty"`
}

// HTTPEnvs answers http.envs: the environments and their variables;
// a private or secret value comes as Hidden with no value.
type HTTPEnvs struct {
	Selected string    `json:"selected,omitempty"`
	Files    []string  `json:"files"`
	Envs     []HTTPEnv `json:"envs"`
}

// HTTPEnv is one environment.
type HTTPEnv struct {
	Name string    `json:"name"`
	Vars []HTTPVar `json:"vars"`
}

// HTTPVar is one variable.
type HTTPVar struct {
	Name   string `json:"name"`
	Value  string `json:"value,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

// HTTPEnvArgs names an environment ("": none).
type HTTPEnvArgs struct {
	Name string `json:"name"`
}

// HTTPPath answers a change with the workspace file it made or opened.
type HTTPPath struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	Size int64  `json:"size,omitempty"`
}

// HTTPSaved answers http.saved: the saved requests (1-based).
type HTTPSaved struct {
	Requests []HTTPReqItem `json:"requests"`
}

// HTTPSaveArgs saves a run's request, or a file's, under Name.
type HTTPSaveArgs struct {
	Run   int    `json:"run,omitempty"`
	Path  string `json:"path,omitempty"`
	Index int    `json:"index,omitempty"`
	Name  string `json:"name"`
}

// HTTPCurlArgs names a run's request, or a file's, to show as curl.
type HTTPCurlArgs struct {
	Run   int    `json:"run,omitempty"`
	Path  string `json:"path,omitempty"`
	Index int    `json:"index,omitempty"`
}

// HTTPCurl answers http.curl: one line, secret values as {{templates}}.
type HTTPCurl struct {
	Curl string `json:"curl"`
}

// HTTPImportArgs adds a curl command line to a .http file ("": the
// workspace's scratch.http).
type HTTPImportArgs struct {
	Curl string `json:"curl"`
	Path string `json:"path,omitempty"`
}

// HTTPNewFileArgs names a new .http file.
type HTTPNewFileArgs struct {
	Path string `json:"path"`
}

func (a *HTTPNewFileArgs) validate(int) error {
	if !validPath(a.Path, false) {
		return invalid("bad path")
	}
	return nil
}

// HTTPSaveBodyArgs writes a run's response body to a new workspace file.
type HTTPSaveBodyArgs struct {
	Run  int    `json:"run,omitempty"`
	Path string `json:"path"`
}

// validHTTPToken is an HTTP token (a method or a header name).
func validHTTPToken(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$%&'*+.^_|~-", c) >= 0) {
			return false
		}
	}
	return true
}

// validHTTPFileRef: a .http file's path with a 1-based index, or neither.
func validHTTPFileRef(path string, index int) bool {
	if path == "" {
		return index == 0
	}
	return validPath(path, false) && index > 0 && index <= 10000
}

func (a *HTTPSendArgs) validate(int) error {
	sources := 0
	if a.Path != "" || a.Index != 0 {
		sources++
		if !validHTTPFileRef(a.Path, a.Index) {
			return invalid("bad .http request")
		}
	}
	if a.History != "" {
		sources++
		if !validHTTPToken(a.History, 64) {
			return invalid("bad history id")
		}
	}
	if a.Saved != 0 {
		sources++
		if a.Saved < 0 || a.Saved > 10000 {
			return invalid("bad saved request")
		}
	}
	typed := a.URL != "" || a.Method != "" || len(a.Headers) > 0 || a.Body != ""
	if typed {
		sources++
		if a.URL == "" || len(a.URL) > MaxHTTPURL || hasControl(a.URL) || a.Method != "" && !validHTTPToken(a.Method, 32) ||
			len(a.Headers) > MaxHTTPHeaders || len(a.Body) > MaxHTTPBody {
			return invalid("bad request")
		}
		for _, h := range a.Headers {
			if !validHTTPToken(h.Name, 256) || len(h.Value) > MaxHTTPHeader || hasControl(h.Value) {
				return invalid("bad header")
			}
		}
	}
	if sources != 1 {
		return invalid("name one request")
	}
	if len(a.Env) > MaxHTTPName || hasControl(a.Env) {
		return invalid("bad environment")
	}
	return nil
}

func (a *HTTPRunArgs) validate(int) error {
	if a.Run < 0 {
		return invalid("bad run")
	}
	return nil
}

func (a *HTTPEnvArgs) validate(int) error {
	if len(a.Name) > MaxHTTPName || hasControl(a.Name) {
		return invalid("bad environment")
	}
	return nil
}

func (a *HTTPSaveArgs) validate(int) error {
	if a.Run < 0 || !validHTTPFileRef(a.Path, a.Index) || !validName(a.Name, MaxHTTPName) {
		return invalid("bad save")
	}
	return nil
}

func (a *HTTPCurlArgs) validate(int) error {
	if a.Run < 0 || !validHTTPFileRef(a.Path, a.Index) {
		return invalid("bad request")
	}
	return nil
}

func (a *HTTPImportArgs) validate(int) error {
	if strings.TrimSpace(a.Curl) == "" || len(a.Curl) > MaxHTTPCurl || a.Path != "" && !validPath(a.Path, false) {
		return invalid("bad import")
	}
	return nil
}

func (a *HTTPSaveBodyArgs) validate(int) error {
	if a.Run < 0 || !validPath(a.Path, false) {
		return invalid("bad path")
	}
	return nil
}
