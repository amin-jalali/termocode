//go:build unix

package webgw

import (
	"runtime"
	"testing"

	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// web_gateway_conn_kib (WEB-27, web design 5.5): the gateway's heap per
// browser connection after hello, with 1 controller and 3 viewers (the
// default cap of 4) open. Both ends live in this test process, so the
// number is an upper bound for the gateway alone.
func TestPerfWebGatewayConnMemory(t *testing.T) {
	perfbudget.Require(t)
	r := newWSRig(t, DefaultLimits, nil)
	heap := func() uint64 {
		var ms runtime.MemStats
		for i := 0; i < 3; i++ {
			runtime.GC()
		}
		runtime.ReadMemStats(&ms)
		return ms.HeapAlloc
	}
	before := heap()
	const n = 4
	r.connect()
	for i := 1; i < n; i++ {
		tok, _, err := r.tokens.Issue("loc-v"+itoa(i), RoleViewer, r.clock.Now())
		if err != nil {
			t.Fatal(err)
		}
		r.connectWith(tok)
	}
	after := heap()
	per := float64(0)
	if after > before {
		per = float64(after-before) / n / 1024
	}
	perfbudget.Check(t, "web_gateway_conn_kib", float64(int(per*10))/10)
}
