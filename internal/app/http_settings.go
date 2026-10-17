package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// HTTP client settings (plan HC-7): config.json keys with Settings rows
// in an "HTTP" group. settingsConfig does not know them; saveSettings
// keeps unknown keys, and loadHTTPSettings reads them.

type httpSettingRow struct {
	id, key, kind, def, desc string
	lo, hi                   int // int rows: allowed range
}

var httpSettingRows = []httpSettingRow{
	{"setting-http-timeout", "http_timeout", "int", "30",
		"Seconds an HTTP request may take (from .http files, Quick Request and the AI's http_send); # @timeout in a request wins.", 1, 3600},
	{"setting-http-max-redirects", "http_max_redirects", "int", "10",
		"Most redirects an HTTP request follows; # @no-redirect in a request shows the 3xx itself.", 1, 50},
	{"setting-http-max-body-mb", "http_max_body_mb", "int", "20",
		"Largest HTTP response body read, in MB; a bigger one is cut and marked truncated.", 1, 1024},
	{"setting-http-ai-max-kb", "http_ai_max_kb", "int", "32",
		"Most KB of a response body the AI sees per http_send / http_get_last_response call (it can page on with offset).", 1, 1024},
	{"setting-http-tls-insecure", "http_tls_insecure", "bool", "false",
		"Skip TLS certificate checks for your own HTTP requests (never for the AI's). Prefer the k retry on the response page, which is per host and per session.", 0, 0},
	{"setting-http-cookie-jar", "http_cookie_jar", "bool", "true",
		"Keep cookies between HTTP requests (in memory only, per session); # @no-cookie-jar in a request turns it off for that request.", 0, 0},
	{"setting-http-history-max", "http_history_max", "int", "200",
		"Most entries kept in http/history.jsonl.", 1, 100000},
}

func init() {
	for _, r := range httpSettingRows {
		settingsRows = append(settingsRows, settingsRow{ID: r.id, Label: r.key, Kind: r.kind, Default: r.def, Desc: r.desc})
	}
}

func httpSettingRowByID(id string) (httpSettingRow, bool) {
	for _, r := range httpSettingRows {
		if r.id == id {
			return r, true
		}
	}
	return httpSettingRow{}, false
}

// httpSettingRange is the allowed range of an http_* int setting.
func httpSettingRange(key string) (lo, hi int, ok bool) {
	for _, r := range httpSettingRows {
		if r.key == key && r.kind == "int" {
			return r.lo, r.hi, true
		}
	}
	return 0, 0, false
}

// httpSettingsCategories puts the rows in their own Settings group.
func httpSettingsCategories(cats map[string]string) map[string]string {
	for _, r := range httpSettingRows {
		cats[r.id] = "HTTP"
	}
	return cats
}

// httpSettingValue renders a row's current value. ok=false: not ours.
func httpSettingValue(id string) (string, bool) {
	r, ok := httpSettingRowByID(id)
	if !ok {
		return "", false
	}
	s := loadHTTPSettings()
	switch r.key {
	case "http_timeout":
		return strconv.Itoa(intOr(s.Timeout, httpDefaultTimeout)), true
	case "http_max_redirects":
		return strconv.Itoa(intOr(s.MaxRedirects, httpclient.DefaultMaxRedirects)), true
	case "http_max_body_mb":
		return strconv.Itoa(intOr(s.MaxBodyMB, httpDefaultMaxBodyMB)), true
	case "http_ai_max_kb":
		return strconv.Itoa(intOr(s.AIMaxKB, httpDefaultAIMaxKB)), true
	case "http_tls_insecure":
		return strconv.FormatBool(s.tlsInsecure()), true
	case "http_cookie_jar":
		return strconv.FormatBool(s.cookieJar()), true
	case "http_history_max":
		return strconv.Itoa(intOr(s.HistoryMax, httpclient.DefaultHistoryMax)), true
	}
	return "", false
}

// applyHTTPSetting validates and saves one row. ok=false: not ours.
func (m *Model) applyHTTPSetting(id, value string) (tea.Cmd, bool) {
	r, ok := httpSettingRowByID(id)
	if !ok {
		return nil, false
	}
	value = strings.TrimSpace(value)
	var v any
	switch r.kind {
	case "bool":
		b, err := parseBoolLoose(value)
		if err != nil {
			return m.pushToastDetail(toast.Errr, r.key+": expected true or false", ""), true
		}
		v, value = b, strconv.FormatBool(b)
	default:
		n, err := strconv.Atoi(value)
		if err != nil || n < r.lo || n > r.hi {
			return m.pushToastDetail(toast.Errr, r.key+": expected an integer "+strconv.Itoa(r.lo)+"…"+strconv.Itoa(r.hi), ""), true
		}
		v = n
	}
	if err := saveHTTPSetting(r.key, v); err != nil {
		return m.pushToastDetail(toast.Errr, "Settings error", err.Error()), true
	}
	return m.pushToastDetail(toast.Info, r.key+" = "+value, ""), true
}

// httpConfigFiles are the HTTP client's rows of ConfigFileCatalog.
func httpConfigFiles() []ConfigFile {
	return []ConfigFile{
		{"config", "http/saved.http", "Requests saved with HTTP: Save Request to Saved… (S on the response page).",
			"A normal `.http` file (the format mobocode reads too). File mode 0600.", "Yes. HTTP: Saved Requests… → Open saved.http."},
		{"config", "http/history.jsonl", "HTTP history: one JSON line per send (yours and the AI's). Holds the request as written (`{{templates}}`, secret headers `[REDACTED]`), never the resolved values; the first 256 KB of a text answer. File mode 0600, folder 0700.",
			"JSON lines, written by termocode; `http_history_max` trims it.", "No (state). HTTP: History… → Clear history…, or delete it."},
		{"config", "http/state.json", "The HTTP environment chosen per workspace (HTTP: Select Environment…).",
			"JSON `{\"envs\": {workspace: env}}`.", "Not needed."},
		{"workspace", httpclient.EnvFile, "HTTP environments for `{{name}}` in `.http` files, next to them or in a parent folder up to the workspace root (the nearest wins). `$shared` applies to every environment.",
			"JSON `{\"$shared\": {...}, \"dev\": {\"host\": \"…\"}, …}` (VS Code REST Client / JetBrains shape).", "Yes. HTTP: Open Environment File creates it."},
		{"workspace", httpclient.PrivateEnvFile, "Secret values (tokens) for the same environments; they win over " + httpclient.EnvFile + ", are hidden from the AI and the logs, and must stay out of git.",
			"Same shape as " + httpclient.EnvFile + ".", "Yes. HTTP: Open Private Environment File creates it (mode 0600) and offers to add it to .gitignore."},
	}
}
