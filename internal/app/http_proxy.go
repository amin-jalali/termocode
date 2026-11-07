package app

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// HTTP: Set Request Proxy… (API-21) writes `# @proxy URL` above the
// request at the cursor (empty removes it, none sends direct). The
// prompt shows what the request uses today: its own @proxy, the
// environment's $proxy or HTTP(S)_PROXY.

const promptKindHTTPProxy promptKindEnum = 5182

func httpProxyPaletteItems() []picker.Item {
	return []picker.Item{{ID: "http-proxy-set", Title: "HTTP: Set Request Proxy…"}}
}

// httpProxyNow says which proxy req uses when sent with env.
func httpProxyNow(req httpclient.Request, envProxy string) string {
	if v := strings.TrimSpace(req.Directives["proxy"]); v != "" {
		return "@proxy " + httpclient.RedactURL(v)
	}
	if envProxy != "" {
		return "$proxy of the environment: " + httpclient.RedactURL(envProxy)
	}
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if v := os.Getenv(k); v != "" {
			return k + ": " + httpclient.RedactURL(v)
		}
	}
	return "direct (no proxy)"
}

// httpProxyLine is the directive line for value v ("" = none).
func httpProxyLine(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return "# @proxy " + v
}

// openHTTPProxyPrompt asks for the proxy of the request at the cursor.
func (m *Model) openHTTPProxyPrompt() tea.Cmd {
	if m.httpCurrentFile() == "" {
		return m.pushToast(toast.Info, "Open an .http file first")
	}
	file, _, req, err := m.httpRequestFromBuffer(0, 0)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "HTTP: "+err.Error(), "")
	}
	env := httpSelectedEnv(httpRootFor(file))
	envProxy := ""
	rv := httpResolver(file, nil, env)
	if rv.Envs != nil {
		envProxy, _, _ = rv.Envs.Lookup(rv.Env, httpclient.EnvProxyKey)
	}
	s := m.httpS()
	s.proxyReq = httpPickReq{file: file, line: req.Line}
	m.openHTTPPrompt(promptKindHTTPProxy, "HTTP: Set Request Proxy · now "+httpProxyNow(*req, envProxy),
		"Proxy (http://, socks5://, none; empty removes):", req.Directives["proxy"])
	return nil
}

// httpProxyPrompt handles the proxy prompt. ok=false: not ours.
func (m *Model) httpProxyPrompt(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindHTTPProxy {
		return nil, false
	}
	value = strings.TrimSpace(value)
	if err := httpclient.CheckProxy(value); err != nil {
		return m.pushToastDetail(toast.Warn, "Not a proxy", err.Error()), true
	}
	s := m.httpS()
	if m.nvim == nil || s.proxyReq.file != m.editor.Path() {
		return m.pushToast(toast.Warn, "The .http file is no longer open"), true
	}
	_, _, req, err := m.httpRequestFromBuffer(0, s.proxyReq.line)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "HTTP: "+err.Error(), ""), true
	}
	if err := m.nvim.ExecLuaArgs(httpProxyLua, req.StartLine, req.Line, httpProxyLine(value)); err != nil {
		return m.pushToastDetail(toast.Errr, "Could not edit the request", err.Error()), true
	}
	m.focus = FocusEditor
	switch {
	case value == "":
		return m.pushToast(toast.Info, "Request proxy removed"), true
	default:
		return m.pushToast(toast.Info, "Request proxy: "+httpclient.RedactURL(value)), true
	}
}

// httpProxyLua replaces, removes or adds the @proxy line between the
// block's first line and the request line (1-based, current buffer).
const httpProxyLua = `local first, reqline, text = ...
if first < 1 or first > reqline then first = reqline end
local lines = vim.api.nvim_buf_get_lines(0, first - 1, reqline - 1, false)
for i, l in ipairs(lines) do
  if l:match('^%s*#%s*@proxy%f[%s%z]') or l:match('^%s*//%s*@proxy%f[%s%z]') then
    local row = first - 1 + i - 1
    if text == '' then
      vim.api.nvim_buf_set_lines(0, row, row + 1, false, {})
    else
      vim.api.nvim_buf_set_lines(0, row, row + 1, false, { text })
    end
    return
  end
end
if text ~= '' then
  vim.api.nvim_buf_set_lines(0, reqline - 1, reqline - 1, false, { text })
end`
