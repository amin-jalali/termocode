package app

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc/codes"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/httpclient/grpcreq"
)

// gRPC from the web page (API-19): a GRPC request runs on the laptop's
// gRPC client with the web's host rules (this machine at once, another
// host after the laptop card, a private address every time; a public
// name may not reach a private address). The answer is the call page as
// a plain-text body: 200 OK when the call ended with status OK, else the
// gRPC status is the answer's error.

// webHTTPGRPCGateURL is the http:// URL of a gRPC request's host:port
// for the web's host gate.
func webHTTPGRPCGateURL(rawURL string) string {
	t, err := grpcreq.ParseTarget(rawURL)
	if err != nil {
		return rawURL
	}
	return "http://" + t.Addr
}

// webHTTPGRPCDo runs the gRPC request rr of file (a test can point it
// at any server; it dials with the guard).
func webHTTPGRPCDo(ctx context.Context, rr httpclient.Resolved, file string, insecureTLS bool) *httpclient.Response {
	start := time.Now()
	out := &httpclient.Response{Method: "GRPC", URL: rr.URL, Proto: "gRPC"}
	hide := func(s string) string {
		for _, v := range rr.Secrets {
			if v != "" {
				s = strings.ReplaceAll(s, v, httpclient.Redacted)
			}
		}
		return s
	}
	plan, err := httpGRPCPlanFor(rr, file, loadHTTPSettings().timeout())
	var res grpcreq.Result
	if err == nil {
		res, err = httpGRPCInvoke(ctx, plan, insecureTLS, httpGRPCGuardDial(plan.target.Addr))
	}
	out.Elapsed = time.Since(start)
	if err != nil {
		out.Err = hide(err.Error())
		return out
	}
	text := hide(res.Text(plan.target))
	out.Body, out.Size, out.ContentType = []byte(text), int64(len(text)), "text/plain"
	if res.Code == codes.OK {
		out.Status, out.StatusText = 200, "OK"
	} else {
		out.Err = hide("gRPC status " + res.Code.String() + ": " + res.Message)
	}
	return out
}
