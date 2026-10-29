package mcp

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

// Spec (SCN-13): a remote MCP server that can not be reached gives a
// *ConnError with the host and the cause in plain words, not the Go
// error ("dial tcp …: connect: connection refused").
func TestStartHTTPUnreachable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	_, err = StartHTTP(context.Background(), Server{Name: "far", URL: "http://" + addr + "/mcp"}, HTTPOptions{}, Options{})
	var ce *ConnError
	if !errors.As(err, &ce) {
		t.Fatalf("want *ConnError, got %T %v", err, err)
	}
	if ce.Host != addr || ce.Why != "it refused the connection. Is the server running?" {
		t.Errorf("got host %q why %q", ce.Host, ce.Why)
	}
	if want := "no connection to " + addr + ": it refused the connection"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q lacks %q", err, want)
	}
}
