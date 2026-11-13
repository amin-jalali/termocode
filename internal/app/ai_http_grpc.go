package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"syscall"

	"google.golang.org/grpc"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/httpclient/grpcreq"
)

// gRPC through the AI's http_send (API-19): a GRPC request (method GRPC
// or a grpc:// / grpcs:// URL) runs on the gRPC client instead of the
// HTTP one. Every call asks (a call may change data), private hosts
// always; the dial refuses a private address for a public name.

// httpGRPCValidateURL checks the URL of a gRPC request (the AI's and
// the web's sends): a host and a
// method.
func httpGRPCValidateURL(rawURL string) error {
	t, err := grpcreq.ParseTarget(rawURL)
	if err != nil {
		return err
	}
	if t.Method == "" {
		return errors.New("grpc: put the method in the URL (grpc://host:port/package.Service/Method); see the services with HTTP: Describe gRPC Services")
	}
	return nil
}

// httpAIGRPCGate is the gate of a gRPC call: the HTTP rule of a write to
// the same host:port.
func httpAIGRPCGate(rawURL string) (bool, string) {
	t, err := grpcreq.ParseTarget(rawURL)
	if err != nil {
		return true, ""
	}
	return httpAIGate("GRPC", "http://"+t.Addr)
}

// httpAIGRPCGuard is the dial check of an AI gRPC call to host: a
// public name may not connect to a private, link-local or metadata
// address; a private host itself (the user allowed it) may.
func httpAIGRPCGuard(host string) func(network, address string, _ syscall.RawConn) error {
	allowAll := httpclient.ClassifyHost(host) == httpclient.HostPrivate
	return func(_, address string, _ syscall.RawConn) error {
		if allowAll {
			return nil
		}
		h, _, err := net.SplitHostPort(address)
		if err != nil {
			h = address
		}
		if ip := net.ParseIP(h); ip != nil && httpclient.IsPrivateIP(ip) {
			return fmt.Errorf("grpc: %s resolves to a private address; it needs its own approval", host)
		}
		return nil
	}
}

// httpGRPCGuardDial dials addr (host:port) with httpAIGRPCGuard.
func httpGRPCGuardDial(addr string) grpc.DialOption {
	host, _, _ := net.SplitHostPort(addr)
	d := &net.Dialer{Control: httpAIGRPCGuard(host)}
	return grpc.WithContextDialer(func(ctx context.Context, a string) (net.Conn, error) {
		return d.DialContext(ctx, "tcp", a)
	})
}

// httpAIGRPCRun runs the gRPC request of r and returns the call page
// for the model; a failed call is the answer, not a Go error.
func httpAIGRPCRun(ctx context.Context, root string, r *httpAIReq, maxBytes int) string {
	set := loadHTTPSettings()
	file := r.file
	if file == "" {
		file = filepath.Join(root, "ad-hoc.http")
	}
	plan, err := httpGRPCPlanFor(r.rr, file, set.timeout())
	text := ""
	if err == nil {
		text, err = httpGRPCCall(ctx, plan, false, httpGRPCGuardDial(plan.target.Addr))
	}
	if err != nil {
		text = "gRPC call failed: " + err.Error()
	}
	for _, v := range r.rr.Secrets {
		if v != "" {
			text = strings.ReplaceAll(text, v, httpclient.Redacted)
		}
	}
	if maxBytes > 0 && len(text) > maxBytes {
		text = strings.ToValidUTF8(text[:maxBytes], "") + fmt.Sprintf("\n… [truncated: %d of %d bytes]", maxBytes, len(text))
	}
	return aiCleanText(ai.Redact(text))
}
