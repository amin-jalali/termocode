package app

import (
	"slices"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// PERF-8 perf guard: "send all" of a 200-request file to a local
// httptest server (answers at once), one request after another: parse,
// resolve (env files, private env, $dotenv, $secret, $guid,
// $timestamp), send, history (1 000 entries). Median of 5.
func TestPerfHTTPSendAll200(t *testing.T) {
	perfbudget.Require(t)
	ws := newHTTPBenchWS(t, 200)
	srv := httpBenchServer(t, []byte(`{"ok":true}`), "application/json")
	var took []time.Duration
	for range 5 {
		t0 := time.Now()
		f := httpclient.Parse(ws.src)
		for _, r := range f.Requests {
			httpBenchSend(t, ws, srv.URL, f, r)
		}
		took = append(took, time.Since(t0))
	}
	slices.Sort(took)
	perfbudget.Check(t, "http_send_all_200", float64(took[len(took)/2].Microseconds()/100)/10)
}
