package app

import (
	"context"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// startGRPCLoopback serves the reflection service on 127.0.0.1 (a real
// TCP port on this machine; no network) and returns its host:port.
func startGRPCLoopback(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	reflection.Register(srv)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

// API-19: http_send routes a GRPC request (from a .http file or ad hoc)
// to the gRPC client: the card names the call, every gRPC call asks
// (it may change data; class http:<host>/write), and the answer is the
// call page (replies and status) for the model.
func TestAIHTTPSendGRPC(t *testing.T) {
	e := covHTTPSetup(t)
	addr := startGRPCLoopback(t)
	e.write(t, "api/g.http", "### list\nGRPC grpc://"+addr+"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo\n\n{\"listServices\": \"*\"}\n")
	send := e.tools["http_send"]
	for _, a := range []map[string]any{
		{"path": "api/g.http", "name": "list"},
		{"method": "GRPC", "url": "grpc://" + addr + "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo", "body": `{"listServices": "*"}`},
	} {
		if err := send.Validate(a); err != nil {
			t.Fatalf("%v: validate: %v", a, err)
		}
		if mut, class := send.Gate(a); !mut || class != "http:"+addr+"/write" {
			t.Errorf("%v: gate = %v %q", a, mut, class)
		}
		if d := send.Describe(a); !strings.HasPrefix(d, "GRPC grpc://"+addr+"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo") {
			t.Errorf("card = %q", d)
		}
		out, err := send.Run(context.Background(), a)
		if err != nil {
			t.Fatal(err)
		}
		covAIMustContain(t, "grpc send", out, "← reply 1", "grpc.reflection.v1alpha.ServerReflection", "status OK")
	}
	// A URL without a method, and a method the server does not have.
	if err := send.Validate(map[string]any{"method": "GRPC", "url": "grpc://" + addr + "/grpc.reflection.v1.ServerReflection"}); err == nil || !strings.Contains(err.Error(), "method") {
		t.Errorf("no method: %v", err)
	}
	out, err := send.Run(context.Background(), map[string]any{"method": "GRPC", "url": "grpc://" + addr + "/grpc.reflection.v1.ServerReflection/Nope"})
	if err != nil || !strings.Contains(out, "gRPC call failed: ") {
		t.Errorf("unknown method: %q %v", out, err)
	}
	// A private network host always asks with no "Always allow".
	if mut, class := send.Gate(map[string]any{"method": "GRPC", "url": "grpc://10.0.0.5:50051/a.B/C"}); !mut || class != "" {
		t.Errorf("private gate = %v %q", mut, class)
	}
}

// The dial of an AI gRPC call to a public name refuses a private
// address (DNS rebinding); a private host the user allowed and this
// machine are fine.
func TestAIHTTPGRPCDialGuard(t *testing.T) {
	for _, c := range []struct {
		host, addr string
		ok         bool
	}{
		{"api.example.com", "10.0.0.5:443", false},
		{"api.example.com", "169.254.169.254:80", false},
		{"api.example.com", "93.184.216.34:443", true},
		{"api.example.com", "127.0.0.1:50051", true},
		{"10.0.0.5", "10.0.0.5:50051", true},
	} {
		err := httpAIGRPCGuard(c.host)("tcp", c.addr, nil)
		if (err == nil) != c.ok {
			t.Errorf("%s → %s: %v", c.host, c.addr, err)
		}
	}
}
