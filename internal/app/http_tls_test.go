package app

import (
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// The user's own send reads the TLS directives of the request; paths
// are read from the .http file's folder. A quick request (no file)
// reads them from nothing.
func TestHTTPUserSendReadsTLSDirectives(t *testing.T) {
	dir := t.TempDir()
	run := &httpRun{file: filepath.Join(dir, "api.http")}
	run.rr.Request = httpclient.Request{Method: "GET", URL: "https://x.test", Directives: map[string]string{
		"insecure": "", "ca-cert": "ca.pem", "client-cert": "me.crt", "client-key": "me.key",
	}}
	var h httpclient.Hooks
	httpUserTLS(run, &h)
	if !h.Insecure || h.CACert != filepath.Join(dir, "ca.pem") ||
		h.ClientCert != filepath.Join(dir, "me.crt") || h.ClientKey != filepath.Join(dir, "me.key") {
		t.Fatalf("hooks %+v", h)
	}
}

// AI sends never skip TLS checks or load the user's client keys.
func TestHTTPAIHooksIgnoreTLSDirectives(t *testing.T) {
	h := httpAIHooks("GET", "https://x.test")
	if h.Insecure || h.CACert != "" || h.ClientCert != "" || h.ClientKey != "" {
		t.Fatalf("AI hooks %+v", h)
	}
}
