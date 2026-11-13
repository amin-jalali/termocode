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
