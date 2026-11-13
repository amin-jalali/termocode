package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (API-19, web side): a GRPC request sent from the page runs on the
// laptop's gRPC client; the answer is the call page (replies, status) as
// the body with status 200 OK. A call that cannot start is the answer's
// error. A URL without a method is refused; a private host waits for the
// laptop card like any HTTP request.
func TestWebHTTPSendGRPC(t *testing.T) {
	r := newWebHTTPRig(t)
	addr := startGRPCLoopback(t)
	svc := "grpc://" + addr + "/grpc.reflection.v1.ServerReflection"
	r.write("api/g.http", "### list\nGRPC "+svc+"/ServerReflectionInfo\n\n{\"listServices\": \"*\"}\n\n### nope\nGRPC "+svc+"/Nope\n")

	var sent webproto.HTTPSent
	webTestData(t, r.do(webproto.OpHTTPSend, `{"path":"api/g.http","index":1}`), &sent)
	if sent.Run == 0 || sent.Card != "" || r.m.confirmOpen {
		t.Fatalf("sent %+v (card open %v)", sent, r.m.confirmOpen)
	}
	var res webproto.HTTPResponse
	webTestData(t, r.do(webproto.OpHTTPResponse, fmt.Sprintf(`{"run":%d}`, sent.Run)), &res)
	if res.Running || res.Err != "" || res.Status != 200 || res.Method != "GRPC" || res.By != "web" ||
		!strings.Contains(res.Body, "← reply 1") || !strings.Contains(res.Body, "grpc.reflection.v1alpha.ServerReflection") || !strings.Contains(res.Body, "status OK") {
		t.Fatalf("response %+v", res)
	}

	sent = webproto.HTTPSent{}
	webTestData(t, r.do(webproto.OpHTTPSend, `{"path":"api/g.http","index":2}`), &sent)
	res = webproto.HTTPResponse{}
	webTestData(t, r.do(webproto.OpHTTPResponse, fmt.Sprintf(`{"run":%d}`, sent.Run)), &res)
	if res.Err == "" || !strings.Contains(res.Err, "Nope") || res.Status != 0 {
		t.Fatalf("unknown method %+v", res)
	}

	// Typed on the page.
	sent = webproto.HTTPSent{}
	webTestData(t, r.do(webproto.OpHTTPSend, `{"method":"GRPC","url":"`+svc+`/ServerReflectionInfo","body":"{\"listServices\": \"*\"}"}`), &sent)
	res = webproto.HTTPResponse{}
	webTestData(t, r.do(webproto.OpHTTPResponse, fmt.Sprintf(`{"run":%d}`, sent.Run)), &res)
	if res.Status != 200 || !strings.Contains(res.Body, "status OK") {
		t.Fatalf("typed %+v", res)
	}

	if f := r.do(webproto.OpHTTPSend, `{"method":"GRPC","url":"`+svc+`"}`); webTestErr(f) != "bad_request" || !strings.Contains(fmt.Sprint(f["msg"]), "method") {
		t.Fatalf("no method: %v", f)
	}
	sent = webproto.HTTPSent{}
	webTestData(t, r.do(webproto.OpHTTPSend, `{"method":"GRPC","url":"grpc://10.0.0.5:50051/a.B/C"}`), &sent)
	if sent.Card == "" || sent.Run != 0 || sent.Host != "10.0.0.5:50051" {
		t.Fatalf("private host %+v", sent)
	}
	webTestLaptopSelect(t, r.m, "cancel")
}
