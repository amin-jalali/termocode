package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (WEBP-HTTP): the HTTP client's reads are open to viewers; every
// change and every send needs a controller and is audited. No row is a
// laptop card: http.send asks the laptop itself, the first time per host.
func TestHTTPRows(t *testing.T) {
	for _, op := range []string{OpHTTPFiles, OpHTTPResponse, OpHTTPHistory, OpHTTPEnvs, OpHTTPSaved, OpHTTPCurl} {
		p, ok := Lookup(op)
		if !ok || p.Role != RoleViewer || p.LaptopCard || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v", op, p)
		}
	}
	for _, op := range []string{OpHTTPSend, OpHTTPCancel, OpHTTPEnvSelect, OpHTTPEnvFile, OpHTTPSave, OpHTTPImportCurl, OpHTTPNewFile, OpHTTPSaveBody} {
		p, ok := Lookup(op)
		if !ok || p.Role != RoleController || p.LaptopCard || p.Audit != AuditYes || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v", op, p)
		}
	}
}

// Spec: a send names one source (a request of a .http file, a history
// entry, a saved request, or method and url); paths are workspace paths;
// methods are tokens; header names are tokens and values one line.
func TestHTTPArgsRules(t *testing.T) {
	good := map[string][]string{
		OpHTTPSend: {`{"path":"api/users.http","index":2}`, `{"method":"POST","url":"https://api.example.com/x","headers":[{"name":"Content-Type","value":"application/json"}],"body":"{}","env":"dev"}`,
			`{"url":"http://localhost:8080/health"}`, `{"history":"h-12ab"}`, `{"saved":1}`, `{"path":"a.http","index":1,"env":"-"}`},
		OpHTTPResponse:   {`{}`, `{"run":3}`},
		OpHTTPCurl:       {`{"run":0}`, `{"path":"a.http","index":1}`},
		OpHTTPEnvSelect:  {`{"name":"dev"}`, `{"name":""}`},
		OpHTTPSave:       {`{"run":2,"name":"list users"}`, `{"path":"a.http","index":1,"name":"x"}`},
		OpHTTPImportCurl: {`{"curl":"curl https://example.com"}`, `{"curl":"curl -X POST https://example.com -d a=1","path":"api/scratch.http"}`},
		OpHTTPNewFile:    {`{"path":"api/new.http"}`},
		OpHTTPSaveBody:   {`{"run":1,"path":"out/users.json"}`},
	}
	bad := map[string][]string{
		OpHTTPSend: {`{}`, `{"path":"a.http"}`, `{"path":"a.http","index":1,"url":"https://x"}`, `{"path":"../a.http","index":1}`, `{"path":"/etc/a.http","index":1}`,
			`{"url":"https://x","method":"GET X"}`, `{"url":"https://x","headers":[{"name":"Bad Name","value":"v"}]}`,
			`{"url":"https://x","headers":[{"name":"X","value":"a\nb"}]}`, `{"url":"https://x\n"}`, `{"saved":-1}`, `{"history":"a b"}`},
		OpHTTPResponse:   {`{"run":-1}`},
		OpHTTPEnvSelect:  {`{"name":"a\nb"}`},
		OpHTTPSave:       {`{"run":1,"name":""}`, `{"run":1,"name":"a\nb"}`},
		OpHTTPImportCurl: {`{"curl":""}`, `{"curl":"curl x","path":"../a.http"}`},
		OpHTTPNewFile:    {`{"path":""}`, `{"path":"../x.http"}`},
		OpHTTPSaveBody:   {`{"run":1,"path":""}`},
	}
	for op, list := range good {
		for _, a := range list {
			if _, err := DecodeArgs(op, json.RawMessage(a)); err != nil {
				t.Errorf("%s %s: %v", op, a, err)
			}
		}
	}
	for op, list := range bad {
		for _, a := range list {
			if _, err := DecodeArgs(op, json.RawMessage(a)); err == nil {
				t.Errorf("%s %s: accepted", op, strings.ReplaceAll(a, "\n", `\n`))
			}
		}
	}
}
