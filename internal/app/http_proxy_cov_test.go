package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// HTTP: Set Request Proxy… shows the proxy in use (the environment's
// $proxy here), writes the @proxy line, replaces it and removes it; a
// bad value or a closed file says why.
func TestCovHTTPProxyPromptEditsRequest(t *testing.T) {
	m, root, _ := covHTTP(t)
	file := filepath.Join(root, "p.http")
	if err := os.WriteFile(file, []byte("###\n# @name a\nGET http://x/a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, httpclient.EnvFile), []byte(`{"dev":{"$proxy":"http://envproxy:3128"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := httpSetSelectedEnv(httpRootFor(root), "dev"); err != nil {
		t.Fatal(err)
	}
	covHTTPEdit(t, m, file, 3)
	covHTTPDispatch(t, m, "http-proxy-set")
	if !m.promptOpen || m.promptKind != promptKindHTTPProxy || m.prompt.Value() != "" {
		t.Fatalf("prompt %v %q", m.promptOpen, m.prompt.Value())
	}
	m.handleHTTPPromptSubmit("ftp://nope")
	if !strings.Contains(covHTTPToast(m), "Not a proxy") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	m.handleHTTPPromptSubmit("http://p1:8080")
	if got := covHTTPBufText(t, m); got != "###\n# @name a\n# @proxy http://p1:8080\nGET http://x/a" {
		t.Errorf("added:\n%s", got)
	}
	if !strings.Contains(covHTTPToast(m), "Request proxy: http://p1:8080") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	m.httpS().proxyReq.line = 4
	m.handleHTTPPromptSubmit("socks5://p2:1080")
	if got := covHTTPBufText(t, m); !strings.Contains(got, "# @proxy socks5://p2:1080\nGET") || strings.Contains(got, "p1") {
		t.Errorf("replaced:\n%s", got)
	}
	m.handleHTTPPromptSubmit("")
	if got := covHTTPBufText(t, m); strings.Contains(got, "@proxy") || !strings.Contains(covHTTPToast(m), "Request proxy removed") {
		t.Errorf("removed:\n%s", got)
	}
	// The prompt is not the proxy prompt: not ours.
	m.promptKind = promptKindHTTPQuick
	if _, ok := m.httpProxyPrompt("x"); ok {
		t.Error("another prompt taken")
	}
	m.promptKind = promptKindHTTPProxy
	// The buffer no longer has a request.
	_ = m.nvim.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, { '# none' })`)
	m.handleHTTPPromptSubmit("http://p3:1")
	if !strings.Contains(covHTTPToast(m), "no request") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	covHTTPReset(m)
	m.openHTTPProxyPrompt()
	if m.promptOpen || !strings.Contains(covHTTPToast(m), "no request") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	// Another file is open now.
	m.editor.SetMeta(filepath.Join(root, "other.http"), false, "http")
	m.handleHTTPPromptSubmit("http://p3:1")
	if !strings.Contains(covHTTPToast(m), "no longer open") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
}
