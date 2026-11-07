package webgw

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The phone's /pair/wait is held open longer than one request may take
// (DefaultLimits.WriteTimeout 10 s < PairWait 25 s). Over the real server
// with TLS (LAN mode) the held answer must still arrive; before, the
// write deadline cut it off and the phone showed "Pairing did not work".
func TestPairWaitOutlivesTheServerWriteTimeout(t *testing.T) {
	r := newPairRig(t)
	r.p.cfg.WaitPoll = 600 * time.Millisecond
	id, _ := r.start("Android · Chrome")

	srv := httptest.NewUnstartedServer(r.p)
	srv.Config.WriteTimeout = 150 * time.Millisecond // like DefaultLimits: shorter than the hold
	srv.StartTLS()
	defer srv.Close()

	res, err := srv.Client().Post(srv.URL+"/pair/wait", "application/json", strings.NewReader(`{"pair_id":"`+id+`"}`))
	if err != nil {
		t.Fatalf("/pair/wait over TLS failed: %v (the write deadline cut the held answer)", err)
	}
	defer res.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		t.Fatalf("/pair/wait body: %v", err)
	}
	if res.StatusCode != http.StatusOK || m["state"] != "waiting" {
		t.Fatalf("/pair/wait = %d %v, want 200 waiting", res.StatusCode, m)
	}
}

// The default hold must fit in the deadline wait gives itself.
func TestPairWaitDeadlineCoversTheHold(t *testing.T) {
	if pairWaitWrite(PairWait) <= PairWait {
		t.Fatalf("pairWaitWrite(%v) = %v: the answer would be cut off", PairWait, pairWaitWrite(PairWait))
	}
}
