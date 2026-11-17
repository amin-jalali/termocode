package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// intPtr is &n (the settings fields are pointers: unset is nil).
func intPtr(n int) *int { return &n }

// Settings below one fall back to their defaults.
func TestCovHTTPSettingsDefaults(t *testing.T) {
	zero := httpSettings{Timeout: intPtr(0), MaxRedirects: intPtr(-1), MaxBodyMB: intPtr(0), AIMaxKB: intPtr(-5), HistoryMax: intPtr(0)}
	if zero.timeout() != httpDefaultTimeout*time.Second || zero.maxRedirects() != httpclient.DefaultMaxRedirects ||
		zero.maxBody() != httpDefaultMaxBodyMB<<20 || zero.aiMaxBytes() != httpDefaultAIMaxKB<<10 || zero.historyMax() != httpclient.DefaultHistoryMax {
		t.Errorf("defaults: %v %v %v %v %v", zero.timeout(), zero.maxRedirects(), zero.maxBody(), zero.aiMaxBytes(), zero.historyMax())
	}
	if (httpSettings{AIMaxKB: intPtr(2)}).aiMaxBytes() != 2048 {
		t.Error("ai max")
	}
}

// The Settings rows show each value, take valid input, refuse bad
// input, and say when config.json cannot be written.
func TestCovHTTPSettingsRows(t *testing.T) {
	m, _, _ := covHTTP(t)
	for _, r := range httpSettingRows {
		if v, ok := httpSettingValue(r.id); !ok || v != r.def {
			t.Errorf("%s = %q (want %q)", r.key, v, r.def)
		}
	}
	if _, ok := httpSettingValue("nope"); ok {
		t.Error("unknown row has a value")
	}
	if _, _, ok := httpSettingRange("http_tls_insecure"); ok {
		t.Error("a bool row has a range")
	}
	if _, ok := m.applyHTTPSetting("nope", "1"); ok {
		t.Error("unknown row applied")
	}
	m.applyHTTPSetting("setting-http-tls-insecure", "maybe")
	if !strings.Contains(covHTTPToast(m), "expected true or false") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	m.applyHTTPSetting("setting-http-timeout", "0")
	if !strings.Contains(covHTTPToast(m), "expected an integer 1…3600") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	m.applyHTTPSetting("setting-http-history-max", "7")
	if v, _ := httpSettingValue("setting-http-history-max"); v != "7" || !strings.Contains(covHTTPToast(m), "http_history_max = 7") {
		t.Errorf("value %q toast %q", v, covHTTPToast(m))
	}
	m.applyHTTPSetting("setting-http-cookie-jar", "no")
	if v, _ := httpSettingValue("setting-http-cookie-jar"); v != "false" {
		t.Errorf("cookie jar %q", v)
	}
	// The settings folder is a file.
	p, err := settingsConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(p)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.applyHTTPSetting("setting-http-timeout", "5")
	if !strings.Contains(covHTTPToast(m), "Settings error") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
}

// The ring keeps the newest runs; findRun looks them up by id (0 =
// last); the shared client is rebuilt when the settings change.
func TestCovHTTPStateRingAndClient(t *testing.T) {
	m, _, _ := covHTTP(t)
	s := m.httpS()
	for i := 1; i <= httpRingMax+3; i++ {
		s.remember(&httpRun{id: i, resp: &httpclient.Response{Status: 200}})
	}
	if len(s.ring) != httpRingMax || s.ring[0].id != 4 {
		t.Fatalf("ring %d first %d", len(s.ring), s.ring[0].id)
	}
	if s.findRun(0) != s.last || s.findRun(5).id != 5 || s.findRun(1) != nil {
		t.Error("findRun")
	}
	c1 := s.httpClient(loadHTTPSettings())
	if s.httpClient(loadHTTPSettings()) != c1 {
		t.Error("the client was rebuilt with the same settings")
	}
	if err := saveHTTPSetting("http_timeout", 7); err != nil {
		t.Fatal(err)
	}
	if s.httpClient(loadHTTPSettings()) == c1 {
		t.Error("the client was kept after a settings change")
	}
	// The status chip with no answer yet.
	s.last = &httpRun{}
	if got := m.httpStatusTexts(); got != nil {
		t.Errorf("chip %q", got)
	}
	// A lens with test results says how they went.
	run := &httpRun{file: "/f.http", label: "x", resp: &httpclient.Response{Status: 200}, tests: []httpclient.AssertResult{{Expr: "a", Pass: true}, {Expr: "b"}}}
	m.httpRecordLens(run)
	if l := s.lens["/f.http\x00x"]; !strings.Contains(l.text, " · ") || strings.Count(l.text, "·") < 2 {
		t.Errorf("lens %q", l.text)
	}
	// Stale events are dropped.
	if c, ok := m.handleHTTPMsg(httpEventMsg{gen: s.gen + 1}); !ok || c != nil {
		t.Error("a stale event was used")
	}
}

// The live feed: the first text goes out at once, the next within
// httpLiveEvery waits for the timer, a full channel drops the text,
// and nothing goes out after stop.
func TestCovHTTPLiveFeed(t *testing.T) {
	ch := make(chan httpEvent, 1)
	f := &httpLiveFeed{ch: ch}
	f.push([]byte("a"))
	if ev := <-ch; string(ev.live) != "a" || ev.progress != 1 {
		t.Fatalf("first %+v", ev)
	}
	f.push([]byte("ab"))
	f.push([]byte("abc")) // the timer is already set
	for got := ""; got != "abc"; {
		select {
		case ev := <-ch: // "ab" first only on a machine slower than httpLiveEvery
			got = string(ev.live)
		case <-time.After(5 * time.Second):
			t.Fatal("the timer never flushed")
		}
	}
	// A full channel: the text is dropped, never blocks.
	ch <- httpEvent{}
	f.mu.Lock()
	f.sendLocked([]byte("x"), 1)
	f.mu.Unlock()
	<-ch
	// A flush with nothing pending, then stop with a timer set.
	f.flush()
	f.mu.Lock()
	f.last = time.Now()
	f.mu.Unlock()
	f.push([]byte("late"))
	f.stop()
	f.push([]byte("after"))
	f.flush()
	select {
	case ev := <-ch:
		t.Fatalf("sent after stop: %+v", ev)
	default:
	}
}

// A streamed answer runs through the progress and live events into
// the page; the TLS setting skips certificate checks for the user.
func TestCovHTTPStreamAndTLSSetting(t *testing.T) {
	m, _, _ := covHTTP(t)
	chunk := strings.Repeat("data: x\n\n", 4000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for i := 0; i < 30; i++ {
			_, _ = w.Write([]byte(chunk))
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	t.Cleanup(srv.Close)
	covSvcHTTPRun(t, m, m.httpSendRequest("", nil, httpclient.Request{Method: "GET", URL: srv.URL + "/events"}, ""))
	s := m.httpS()
	if s.last == nil || s.last.resp == nil || s.last.resp.Status != 200 || len(s.last.resp.Body) == 0 {
		t.Fatalf("stream: %+v", s.last)
	}
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	t.Cleanup(tls.Close)
	if err := saveHTTPSetting("http_tls_insecure", true); err != nil {
		t.Fatal(err)
	}
	covSvcHTTPRun(t, m, m.httpSendRequest("", nil, httpclient.Request{Method: "GET", URL: tls.URL + "/"}, ""))
	if s.last.resp.Status != 200 {
		t.Errorf("insecure setting: %+v", s.last.resp)
	}
}
