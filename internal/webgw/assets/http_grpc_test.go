package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (API-19, web side): the page's new-request form offers GRPC as a
// method (the laptop runs it on its gRPC client), in the source and in
// the built workbench.js.
func TestHTTPFormOffersGRPC(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("src", "views", "http", "HTTPView.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	line := ""
	for _, l := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(l, "const METHODS = ") {
			line = l
		}
	}
	if !strings.Contains(line, `"GRPC"`) {
		t.Errorf("the method list has no GRPC: %q", line)
	}
	built, err := os.ReadFile("workbench.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(built), `"OPTIONS","GRPC"`) {
		t.Error("workbench.js is not rebuilt with GRPC in the method list (go generate ./internal/webgw/assets)")
	}
}

// Spec (API-19, web side): in the new-request form the headers box takes
// @proto and @proto-path lines (as a .http file does) for a GRPC request;
// they go to the laptop as the proto and proto_dirs arguments and the box's
// hint says so, in English and in Persian.
func TestHTTPFormTakesProtoLines(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("src", "views", "http", "HTTPView.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"@proto"`, `"@proto-path"`, `args.proto =`, `args.proto_dirs =`} {
		if !strings.Contains(string(src), want) {
			t.Errorf("HTTPView.tsx lacks %s", want)
		}
	}
	built, err := os.ReadFile("workbench.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(built), "proto_dirs") {
		t.Error("workbench.js is not rebuilt with the @proto lines (go generate ./internal/webgw/assets)")
	}
	for _, f := range []string{"strings.desk.ts", "strings.fa.ts"} {
		b, err := os.ReadFile(filepath.Join("src", f))
		if err != nil {
			t.Fatal(err)
		}
		line := ""
		for _, l := range strings.Split(string(b), "\n") {
			if strings.Contains(l, `"http.headers":`) {
				line = l
			}
		}
		if !strings.Contains(line, "@proto") {
			t.Errorf("%s: the headers hint does not name @proto: %q", f, line)
		}
	}
}
