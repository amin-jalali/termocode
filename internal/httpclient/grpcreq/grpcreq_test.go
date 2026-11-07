package grpcreq

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	rv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

const demoProto = `// The demo service of the gRPC tests.
syntax = "proto3";
package demo;
option go_package = "example.com/demo;demo";

import "google/protobuf/timestamp.proto";
import "common/mood.proto";

/* Echo says things back. */
service Echo {
  option deprecated = false;
  rpc Say(SayRequest) returns (SayReply);
  rpc Count(CountRequest) returns (stream SayReply) { option deprecated = true; }
  rpc Collect(stream SayRequest) returns (SayReply);
}

message SayRequest {
  string text = 1 [json_name = "text", deprecated = false];
  map<string, int32> tag_counts = 2;
  optional int32 times = 3;
  oneof who {
    string name = 4;
    int64 id = 5;
  }
  common.Mood mood = 6;
  google.protobuf.Timestamp at = 7;
  message Inner { repeated bytes data = 1; }
  Inner inner = 8;
  reserved 9, 10;
  reserved "old";
}

message SayReply { string text = 1; }
message CountRequest { int32 n = 1; }
`

const moodProto = `syntax = "proto3";
package common;
enum Mood {
  option allow_alias = true;
  MOOD_UNSPECIFIED = 0;
  HAPPY = 1;
  GLAD = 1;
  SAD = -1;
}
`

func fakeFS(files map[string]string) readFileFunc {
	return func(p string) ([]byte, error) {
		if s, ok := files[p]; ok {
			return []byte(s), nil
		}
		return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
	}
}

func demoFiles(t *testing.T) *protoregistry.Files {
	t.Helper()
	files, err := loadProtos([]string{"/work/api/demo.proto"}, []string{"/work/include"}, fakeFS(map[string]string{
		"/work/api/demo.proto":            demoProto,
		"/work/include/common/mood.proto": moodProto,
	}))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestParseTarget(t *testing.T) {
	cases := []struct {
		in   string
		want Target
	}{
		{"grpc://localhost:50051/demo.Echo/Say", Target{Addr: "localhost:50051", Service: "demo.Echo", Method: "Say"}},
		{"grpcs://api.example.com/demo.Echo/Say", Target{Addr: "api.example.com:443", TLS: true, Service: "demo.Echo", Method: "Say"}},
		{"localhost:9000/demo.Echo/Say", Target{Addr: "localhost:9000", Service: "demo.Echo", Method: "Say"}},
		{"grpc://h/demo.Echo", Target{Addr: "h:80", Service: "demo.Echo"}},
		{"grpc://[::1]:7000/", Target{Addr: "[::1]:7000"}},
		{"https://h:8443/a.B/C", Target{Addr: "h:8443", TLS: true, Service: "a.B", Method: "C"}},
	}
	for _, c := range cases {
		got, err := ParseTarget(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseTarget(%q) = %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "grpc:///demo.Echo/Say", "ftp://h/a.B/C"} {
		if _, err := ParseTarget(bad); err == nil {
			t.Errorf("ParseTarget(%q): no error", bad)
		}
	}
	if s := (Target{Addr: "h:1", TLS: true}).String(); s != "grpcs://h:1" {
		t.Errorf("String = %q", s)
	}
}

// The parser reads services, streams, maps, oneofs, proto3 optional,
// nested messages, aliased enums and imports (local and well-known).
func TestLoadProtos(t *testing.T) {
	files := demoFiles(t)
	md, err := FindMethod(files, "demo.Echo", "Count")
	if err != nil {
		t.Fatal(err)
	}
	if !md.IsStreamingServer() || md.IsStreamingClient() || md.Output().FullName() != "demo.SayReply" {
		t.Fatalf("Count = %v", md)
	}
	col, _ := FindMethod(files, "demo.Echo", "Collect")
	if col == nil || !col.IsStreamingClient() {
		t.Fatal("Collect is not client streaming")
	}
	in, _ := FindMethod(files, "demo.Echo", "Say")
	f := in.Input().Fields()
	if !f.ByName("tag_counts").IsMap() || f.ByName("tag_counts").MapValue().Kind() != protoreflect.Int32Kind {
		t.Error("tag_counts is not map<string, int32>")
	}
	if !f.ByName("times").HasOptionalKeyword() {
		t.Error("times is not optional")
	}
	if o := in.Input().Oneofs().ByName("who"); o == nil || o.Fields().Len() != 2 {
		t.Error("oneof who has not 2 fields")
	}
	if f.ByName("mood").Enum().FullName() != "common.Mood" || f.ByName("at").Message().FullName() != "google.protobuf.Timestamp" {
		t.Error("imported types not resolved")
	}
	if f.ByName("inner").Message().FullName() != "demo.SayRequest.Inner" {
		t.Error("nested type not resolved")
	}
	if got := Services(files); len(got) != 1 || got[0] != "demo.Echo" {
		t.Errorf("Services = %v", got)
	}
	if _, err := FindMethod(files, "demo.Echo", "Nope"); err == nil || !strings.Contains(err.Error(), "Say, Count, Collect") {
		t.Errorf("unknown method error = %v", err)
	}
	if _, err := FindMethod(files, "demo.Nope", "Say"); err == nil || !strings.Contains(err.Error(), "demo.Echo") {
		t.Errorf("unknown service error = %v", err)
	}
}

func TestLoadProtosErrors(t *testing.T) {
	read := fakeFS(map[string]string{
		"/a.proto": `syntax = "proto3"; import "missing.proto"; message A {}`,
		"/b.proto": `edition = "2023"; message B {}`,
		"/c.proto": `syntax = "proto3"; message C { string s = 1 }`,
		"/d.proto": `syntax = "proto3"; import "../etc/x.proto";`,
	})
	for path, want := range map[string]string{
		"/a.proto": `import "missing.proto" not found`,
		"/b.proto": "editions are not supported",
		"/c.proto": "line 1",
		"/d.proto": "leaves the import folders",
	} {
		_, err := loadProtos([]string{path}, nil, read)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", path, err, want)
		}
	}
	if _, err := loadProtos(nil, nil, read); err == nil {
		t.Error("no files: no error")
	}
}

func TestMetadata(t *testing.T) {
	md := Metadata([][2]string{{"Authorization", "Bearer x"}, {"Content-Type", "application/json"}, {"X-Trace", "1"}, {"x-trace", "2"}})
	if len(md) != 2 || md.Get("authorization")[0] != "Bearer x" || len(md.Get("x-trace")) != 2 {
		t.Fatalf("md = %v", md)
	}
}

func TestDescribe(t *testing.T) {
	text, err := Describe(demoFiles(t), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"service demo.Echo {",
		"rpc Count(demo.CountRequest) returns (stream demo.SayReply);",
		"rpc Collect(stream demo.SayRequest) returns (demo.SayReply);",
		"map<string, int32> tag_counts = 2;",
		"optional int32 times = 3;",
		"google.protobuf.Timestamp at = 7;",
		"message demo.SayReply {",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("Describe has no %q:\n%s", want, text)
		}
	}
	if _, err := Describe(demoFiles(t), "demo.Nope"); err == nil {
		t.Error("unknown service: no error")
	}
}

// demoServices is what the test reflection server lists.
type demoServices struct{}

func (demoServices) GetServiceInfo() map[string]grpc.ServiceInfo {
	return map[string]grpc.ServiceInfo{"demo.Echo": {}}
}

// demoHandler serves demo.Echo from the descriptors alone.
func demoHandler(files *protoregistry.Files, st grpc.ServerStream) error {
	full, _ := grpc.MethodFromServerStream(st)
	svc, name, _ := strings.Cut(strings.TrimPrefix(full, "/"), "/")
	md, err := FindMethod(files, svc, name)
	if err != nil {
		return status.Error(codes.Unimplemented, "no such method")
	}
	text := func(m *dynamicpb.Message) string { return m.Get(md.Input().Fields().ByName("text")).String() }
	reply := func(s string) error {
		r := dynamicpb.NewMessage(md.Output())
		r.Set(md.Output().Fields().ByName("text"), protoreflect.ValueOfString(s))
		return st.SendMsg(r)
	}
	switch name {
	case "Say":
		in := dynamicpb.NewMessage(md.Input())
		if err := st.RecvMsg(in); err != nil {
			return err
		}
		if text(in) == "fail" {
			return status.Error(codes.InvalidArgument, "no fail")
		}
		tok := ""
		if m, ok := metadata.FromIncomingContext(st.Context()); ok && len(m.Get("x-token")) > 0 {
			tok = " " + m.Get("x-token")[0]
		}
		_ = st.SetHeader(metadata.Pairs("x-served", "yes"))
		st.SetTrailer(metadata.Pairs("x-done", "1"))
		return reply("hi " + text(in) + tok)
	case "Count":
		in := dynamicpb.NewMessage(md.Input())
		if err := st.RecvMsg(in); err != nil {
			return err
		}
		n := in.Get(md.Input().Fields().ByName("n")).Int()
		for i := int64(0); i < n; i++ {
			if err := reply(strings.Repeat("#", int(i)+1)); err != nil {
				return err
			}
		}
		return nil
	case "Collect":
		var all []string
		for {
			in := dynamicpb.NewMessage(md.Input())
			err := st.RecvMsg(in)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			all = append(all, text(in))
		}
		return reply(strings.Join(all, "+"))
	}
	return status.Error(codes.Unimplemented, name)
}

// startDemo serves demo.Echo over an in-memory listener; reflect adds
// the reflection service.
func startDemo(t *testing.T, files *protoregistry.Files, reflect bool) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, st grpc.ServerStream) error { return demoHandler(files, st) }))
	if reflect {
		rv1.RegisterServerReflectionServer(srv, reflection.NewServerV1(reflection.ServerOptions{Services: demoServices{}, DescriptorResolver: files}))
	}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	cc, err := Dial(Target{Addr: "bufnet:1"}, false, grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cc.Close() })
	return cc
}

func ctx5(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func replyText(t *testing.T, js string) string {
	t.Helper()
	var v struct{ Text string }
	if err := json.Unmarshal([]byte(js), &v); err != nil {
		t.Fatalf("reply %q: %v", js, err)
	}
	return v.Text
}

// Reflection lists the services and fetches their files (imports
// included); a unary call sends metadata and shows header and trailer.
func TestReflectionAndUnary(t *testing.T) {
	cc := startDemo(t, demoFiles(t), true)
	r := NewReflector(cc, nil)
	svcs, err := r.Services(ctx5(t))
	if err != nil || len(svcs) != 1 || svcs[0] != "demo.Echo" {
		t.Fatalf("Services = %v, %v", svcs, err)
	}
	files, err := r.Files(ctx5(t), "demo.Echo")
	if err != nil {
		t.Fatal(err)
	}
	md, err := FindMethod(files, "demo.Echo", "Say")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Call(ctx5(t), cc, files, md, []string{`{"text":"ann","at":"2026-01-02T03:04:05Z","mood":"HAPPY","tagCounts":{"a":1}}`}, Metadata([][2]string{{"X-Token", "t1"}}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != codes.OK || len(res.Replies) != 1 || replyText(t, res.Replies[0]) != "hi ann t1" {
		t.Fatalf("res = %+v", res)
	}
	if res.Header.Get("x-served")[0] != "yes" || res.Trailer.Get("x-done")[0] != "1" {
		t.Fatalf("header %v trailer %v", res.Header, res.Trailer)
	}
	page := res.Text(Target{Addr: "h:1"})
	for _, want := range []string{"gRPC grpc://h:1/demo.Echo/Say", "→ request 1", "← reply 1", "status OK", "x-served: yes", "x-done: 1"} {
		if !strings.Contains(page, want) {
			t.Errorf("page has no %q:\n%s", want, page)
		}
	}
	// No symbols: every listed service.
	if all, err := r.Files(ctx5(t)); err != nil || len(Services(all)) != 1 {
		t.Fatalf("Files() = %v, %v", all, err)
	}
}

func TestStreams(t *testing.T) {
	files := demoFiles(t)
	cc := startDemo(t, files, false)
	count, _ := FindMethod(files, "demo.Echo", "Count")
	res, err := Call(ctx5(t), cc, files, count, []string{`{"n":3}`}, nil)
	if err != nil || res.Code != codes.OK || len(res.Replies) != 3 || replyText(t, res.Replies[2]) != "###" {
		t.Fatalf("Count = %+v, %v", res, err)
	}
	col, _ := FindMethod(files, "demo.Echo", "Collect")
	res, err = Call(ctx5(t), cc, files, col, []string{`{"text":"a"}`, `{"text":"b"}`}, nil)
	if err != nil || len(res.Replies) != 1 || replyText(t, res.Replies[0]) != "a+b" {
		t.Fatalf("Collect = %+v, %v", res, err)
	}
}

func TestCallErrors(t *testing.T) {
	files := demoFiles(t)
	cc := startDemo(t, files, false)
	say, _ := FindMethod(files, "demo.Echo", "Say")
	res, err := Call(ctx5(t), cc, files, say, []string{`{"text":"fail"}`}, nil)
	if err != nil || res.Code != codes.InvalidArgument || res.Message != "no fail" {
		t.Fatalf("fail = %+v, %v", res, err)
	}
	if !strings.Contains(res.Text(Target{Addr: "h:1"}), "status InvalidArgument: no fail") {
		t.Error("the page does not show the status")
	}
	if _, err := Call(ctx5(t), cc, files, say, []string{`{}`, `{}`}, nil); err == nil || !strings.Contains(err.Error(), "takes one message") {
		t.Errorf("two messages: %v", err)
	}
	if _, err := Call(ctx5(t), cc, files, say, []string{`{"nope":1}`}, nil); err == nil || !strings.Contains(err.Error(), "not a valid demo.SayRequest") {
		t.Errorf("bad JSON: %v", err)
	}
	// No body on a unary call sends an empty message.
	if res, err := Call(ctx5(t), cc, files, say, nil, nil); err != nil || res.Code != codes.OK || replyText(t, res.Replies[0]) != "hi " {
		t.Errorf("empty = %+v, %v", res, err)
	}
}

// A server with no reflection service says so plainly.
func TestNoReflection(t *testing.T) {
	cc := startDemo(t, demoFiles(t), false)
	if _, err := NewReflector(cc, nil).Services(ctx5(t)); !errors.Is(err, ErrNoReflection) {
		t.Fatalf("err = %v", err)
	}
}
