package app

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// covGRPCServer serves only the reflection service on loopback TCP.
func covGRPCServer(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	reflection.Register(srv)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	return l.Addr().String()
}

// The send key and HTTP: Describe gRPC Services on GRPC requests: the
// call and the service list open as pages with the secret token
// hidden; bad requests say why; one call runs at a time.
func TestCovHTTPGRPCFromBuffer(t *testing.T) {
	m, root, _ := covHTTP(t)
	addr := covGRPCServer(t)
	if err := os.WriteFile(filepath.Join(root, httpclient.PrivateEnvFile), []byte(`{"dev":{"tok":"sekret-grpc-tok"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, httpclient.EnvFile), []byte(`{"dev":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := httpSetSelectedEnv(httpRootFor(root), "dev"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "g.http")
	src := "###\n# @name refl\nGRPC grpc://" + addr + "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo\nauthorization: Bearer {{tok}}\n\n{\"listServices\": \"sekret-grpc-tok\"}\n" + // 1-6
		"\n###\n# @name unknown\nGRPC grpc://" + addr + "/a.B/{{nope}}\n" + // 8-10
		"\n###\n# @name bad\nGRPC ftp://" + addr + "/a.B/C\n" + // 12-14
		"\n###\n# @name nomethod\nGRPC grpc://" + addr + "/grpc.reflection.v1.ServerReflection\nauthorization: Bearer {{tok}}\n" + // 16-19
		"\n###\n# @name plain\nGET http://127.0.0.1:1/x\n" + // 21-23
		"\n###\n# @name missing\nGRPC grpc://" + addr + "/no.Such/Thing\nauthorization: Bearer {{tok}}\n" // 25-28
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	loop := newCovHTTPLoop(t, m)
	s := m.httpS()
	page := func() (string, string) {
		var name, text string
		_ = m.nvim.ExecLuaResult(`return vim.api.nvim_buf_get_name(0)`, &name)
		_ = m.nvim.ExecLuaResult(`return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '\n')`, &text)
		return name, text
	}
	// Warnings.
	for line, want := range map[int]string{10: "gRPC: unknown variable {{nope}}", 14: "grpc", 18: "names no method"} {
		covHTTPEdit(t, m, file, line)
		covHTTPReset(m)
		c := covHTTPDispatch(t, m, "http-send")
		loop.run(c, func() bool { return !s.grpcRunning })
		if got := covHTTPToast(m); !strings.Contains(got, want) {
			t.Errorf("line %d: %q", line, got)
		}
	}
	// The call.
	covHTTPEdit(t, m, file, 3)
	c := covHTTPDispatch(t, m, "http-send")
	if !s.grpcRunning {
		t.Fatal("not running")
	}
	covHTTPReset(m)
	m.httpGRPCSend(file, nil, httpclient.Request{})
	if !strings.Contains(covHTTPToast(m), "still running") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-grpc-describe")
	if !strings.Contains(covHTTPToast(m), "still running") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	loop.run(c, func() bool { return !s.grpcRunning })
	name, text := page()
	if !strings.HasSuffix(name, "gRPC: refl") || !strings.Contains(text, "status") || strings.Contains(text, "sekret-grpc-tok") {
		t.Errorf("call page %q:\n%s", name, text)
	}
	// Describe: the named service, then a method that is not there.
	covHTTPEdit(t, m, file, 18)
	c = covHTTPDispatch(t, m, "http-grpc-describe")
	loop.run(c, func() bool { return !s.grpcRunning })
	name, text = page()
	if !strings.HasSuffix(name, "gRPC services: "+addr) || !strings.Contains(text, "rpc ServerReflectionInfo") {
		t.Errorf("describe page %q:\n%s", name, text)
	}
	covHTTPEdit(t, m, file, 27)
	covHTTPReset(m)
	c = covHTTPDispatch(t, m, "http-send")
	loop.run(c, func() bool { return !s.grpcRunning })
	if got := covHTTPToast(m); !strings.Contains(got, "gRPC: missing failed") {
		t.Errorf("toast %q", got)
	}
	covHTTPReset(m)
	c = covHTTPDispatch(t, m, "http-grpc-describe")
	loop.run(c, func() bool { return !s.grpcRunning })
	if got := covHTTPToast(m); !strings.Contains(got, "failed") {
		t.Errorf("toast %q", got)
	}
	// Describe on other requests.
	covHTTPEdit(t, m, file, 23)
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-grpc-describe")
	if !strings.Contains(covHTTPToast(m), "not a gRPC request") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	covHTTPEdit(t, m, file, 10)
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-grpc-describe")
	if !strings.Contains(covHTTPToast(m), "unknown variable") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	m.editor.SetMeta(filepath.Join(root, "x.txt"), false, "text")
	_ = m.nvim.ExecLua(`vim.cmd('enew')`)
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-grpc-describe")
	if !strings.Contains(covHTTPToast(m), "HTTP:") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	// A secret in an error is hidden too.
	cmd := m.httpGRPCRun("t", "n", []string{"", "s3cr3t"}, func() (string, error) { return "a s3cr3t", errors.New("bad s3cr3t") })
	loop.run(cmd, func() bool { return !s.grpcRunning })
	if got := covHTTPToast(m); !strings.Contains(got, "bad "+httpclient.Redacted) || strings.Contains(got, "s3cr3t") {
		t.Errorf("toast %q", got)
	}
}

// Describe and call with .proto files: a broken file or an unknown
// service says why; the server list without a service names them all.
func TestCovHTTPGRPCProtoErrors(t *testing.T) {
	addr := covGRPCServer(t)
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.proto")
	if err := os.WriteFile(bad, []byte("syntax = \"proto3\";\nservice {"), 0o600); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "demo.proto")
	if err := os.WriteFile(good, []byte("syntax = \"proto3\";\npackage demo;\nservice Echo { rpc Say(M) returns (M); }\nmessage M { string text = 1; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	p := httpGRPCTestPlan(t, "grpc://"+addr+"/demo.Echo/Say", `{"text":"hi"}`)
	p.protos = []string{bad}
	if _, err := httpGRPCDescribe(ctx, p, false); err == nil {
		t.Error("describe with a broken proto: no error")
	}
	if _, err := httpGRPCCall(ctx, p, false); err == nil {
		t.Error("call with a broken proto: no error")
	}
	p.protos = []string{good}
	p.target.Service = "no.Such"
	if _, err := httpGRPCDescribe(ctx, p, false); err == nil {
		t.Error("describe of an unknown service: no error")
	}
	// Reflection over the whole server, with metadata: it has no
	// services of its own.
	p = httpGRPCTestPlan(t, "grpc://"+addr, "")
	p.meta = map[string][]string{"x-a": {"b"}}
	// (the reflector leaves the reflection service itself out)
	if _, err := httpGRPCDescribe(ctx, p, false); err == nil || !strings.Contains(err.Error(), "no services") {
		t.Errorf("describe all: %v", err)
	}
	// A plan without a timeout gets 30 s.
	pl, err := httpGRPCPlanFor(httpclient.Resolved{Request: httpclient.Request{Method: "GRPC", URL: "grpc://" + addr + "/a.B/C"}}, filepath.Join(dir, "a.http"), 0)
	if err != nil || pl.timeout != 30*time.Second {
		t.Errorf("timeout %v %v", pl.timeout, err)
	}
}
