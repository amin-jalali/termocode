package app

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/test/bufconn"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

func TestIsGRPCRequest(t *testing.T) {
	for _, r := range []httpclient.Request{{Method: "GRPC", URL: "localhost:50051/a.B/C"}, {Method: "GET", URL: "grpcs://h/a.B/C"}, {Method: "grpc", URL: "grpc://h"}} {
		if !isGRPCRequest(r) {
			t.Errorf("%+v: not gRPC", r)
		}
	}
	if isGRPCRequest(httpclient.Request{Method: "POST", URL: "https://h/a.B/C"}) {
		t.Error("https POST is not gRPC")
	}
}

// The plan: @proto and @proto-path are read next to the .http file,
// headers become metadata (Content-Type left out), the body splits into
// messages on === lines.
func TestHTTPGRPCPlan(t *testing.T) {
	body := "{\"a\":1}\n===\n{\"a\":2}"
	rr := httpclient.Resolved{Request: httpclient.Request{
		Method:     "GRPC",
		URL:        "grpc://h:1/demo.Echo/Collect",
		Headers:    []httpclient.Header{{Name: "Authorization", Value: "Bearer x"}, {Name: "Content-Type", Value: "application/json"}},
		Body:       &body,
		Directives: map[string]string{"proto": "api/demo.proto, /abs/other.proto", "proto-path": "include"},
	}}
	p, err := httpGRPCPlanFor(rr, "/work/req/calls.http", 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.protos, "|") != "/work/req/api/demo.proto|/abs/other.proto" || strings.Join(p.dirs, "|") != "/work/req/include" {
		t.Errorf("protos %v dirs %v", p.protos, p.dirs)
	}
	if p.meta.Get("authorization")[0] != "Bearer x" || len(p.meta.Get("content-type")) != 0 {
		t.Errorf("meta = %v", p.meta)
	}
	if len(p.messages) != 2 || p.timeout != 30*time.Second || p.target.Service != "demo.Echo" || p.target.Method != "Collect" {
		t.Errorf("plan = %+v", p)
	}
	rr.URL = "ftp://h/a.B/C"
	if _, err := httpGRPCPlanFor(rr, "/work/x.http", 0); err == nil {
		t.Error("ftp URL: no error")
	}
}

// startGRPCReflection serves only the reflection service in memory: a
// real bidi-streaming gRPC service whose descriptors come from the
// server itself.
func startGRPCReflection(t *testing.T) grpc.DialOption {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	reflection.Register(srv)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) })
}

func httpGRPCTestPlan(t *testing.T, url, body string) httpGRPCPlan {
	t.Helper()
	rr := httpclient.Resolved{Request: httpclient.Request{Method: "GRPC", URL: url}}
	if body != "" {
		rr.Body = &body
	}
	p, err := httpGRPCPlanFor(rr, filepath.Join(t.TempDir(), "a.http"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A send with no @proto asks the server's reflection for the method and
// shows the replies of a bidi stream and the status.
func TestHTTPGRPCCallByReflection(t *testing.T) {
	dial := startGRPCReflection(t)
	plan := httpGRPCTestPlan(t, "grpc://bufnet:1/grpc.reflection.v1.ServerReflection/ServerReflectionInfo", `{"listServices": "*"}`)
	text, err := httpGRPCCall(context.Background(), plan, false, dial)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"gRPC grpc://bufnet:1/grpc.reflection.v1.ServerReflection/ServerReflectionInfo", "← reply 1", "grpc.reflection.v1alpha.ServerReflection", "status OK"} {
		if !strings.Contains(text, want) {
			t.Errorf("page has no %q:\n%s", want, text)
		}
	}
	plan.target.Method = ""
	if _, err := httpGRPCCall(context.Background(), plan, false, dial); err == nil {
		t.Error("no method: no error")
	}
	plan.target.Method = "Nope"
	if _, err := httpGRPCCall(context.Background(), plan, false, dial); err == nil || !strings.Contains(err.Error(), "ServerReflectionInfo") {
		t.Errorf("unknown method: %v", err)
	}
}

func TestHTTPGRPCDescribe(t *testing.T) {
	dial := startGRPCReflection(t)
	plan := httpGRPCTestPlan(t, "grpc://bufnet:1/grpc.reflection.v1.ServerReflection", "")
	text, err := httpGRPCDescribe(context.Background(), plan, false, dial)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "rpc ServerReflectionInfo(stream grpc.reflection.v1.ServerReflectionRequest) returns (stream grpc.reflection.v1.ServerReflectionResponse);") ||
		!strings.Contains(text, "(from server reflection)") {
		t.Errorf("describe:\n%s", text)
	}
	// With a .proto file nothing is dialed.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "demo.proto"), []byte("syntax = \"proto3\";\npackage demo;\nservice Echo { rpc Say(M) returns (M); }\nmessage M { string text = 1; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan = httpGRPCTestPlan(t, "grpc://nowhere.invalid:1", "")
	plan.protos = []string{filepath.Join(dir, "demo.proto")}
	text, err = httpGRPCDescribe(context.Background(), plan, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "service demo.Echo {") || !strings.Contains(text, "(from demo.proto)") {
		t.Errorf("describe from file:\n%s", text)
	}
}

func TestHTTPGRPCPaletteAndPolicy(t *testing.T) {
	found := false
	for _, it := range httpPaletteItems() {
		found = found || it.ID == "http-grpc-describe"
	}
	if !found {
		t.Error("http-grpc-describe not in the palette")
	}
	if _, ok := aiActionPolicyFor("http-grpc-describe"); !ok {
		t.Error("http-grpc-describe has no AI policy")
	}
}
