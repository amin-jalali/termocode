package grpcreq

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	grpcinsecure "google.golang.org/grpc/credentials/insecure"
)

// Target is the server and method of one gRPC request line:
//
//	GRPC grpcs://api.example.com/helloworld.Greeter/SayHello
//	GRPC localhost:50051/helloworld.Greeter/SayHello
//
// grpc:// (or no scheme) is plain HTTP/2; grpcs:// is TLS.
type Target struct {
	Addr    string // host:port
	TLS     bool
	Service string // full name, e.g. helloworld.Greeter ("" = none)
	Method  string // e.g. SayHello ("" = none)
}

// String is the server part, scheme included.
func (t Target) String() string {
	if t.TLS {
		return "grpcs://" + t.Addr
	}
	return "grpc://" + t.Addr
}

// FullMethod is "/service/method".
func (t Target) FullMethod() string { return "/" + t.Service + "/" + t.Method }

// ParseTarget reads the URL of a gRPC request. With no port, grpcs uses
// 443 and grpc uses 80. The path is service/method; a path with no
// slash names only a service (enough to describe it).
func ParseTarget(raw string) (Target, error) {
	s := strings.TrimSpace(raw)
	var t Target
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "grpcs://"):
		t.TLS, s = true, s[len("grpcs://"):]
	case strings.HasPrefix(low, "grpc://"):
		s = s[len("grpc://"):]
	case strings.HasPrefix(low, "https://"):
		t.TLS, s = true, s[len("https://"):]
	case strings.HasPrefix(low, "http://"):
		s = s[len("http://"):]
	case strings.Contains(low, "://"):
		return Target{}, fmt.Errorf("grpc: use grpc:// or grpcs://, not %q", s[:strings.Index(s, "://")+3])
	}
	host, path, _ := strings.Cut(s, "/")
	if host == "" {
		return Target{}, errors.New("grpc: the URL has no host (grpc://host:port/package.Service/Method)")
	}
	if _, _, err := net.SplitHostPort(host); err != nil {
		port := "80"
		if t.TLS {
			port = "443"
		}
		host = net.JoinHostPort(strings.Trim(host, "[]"), port)
	}
	t.Addr = host
	path = strings.Trim(path, "/")
	if path != "" {
		if i := strings.LastIndex(path, "/"); i >= 0 {
			t.Service, t.Method = path[:i], path[i+1:]
		} else {
			t.Service = path
		}
		if strings.Contains(t.Service, "/") || t.Service == "" {
			return Target{}, fmt.Errorf("grpc: %q is not package.Service/Method", path)
		}
	}
	return t, nil
}

// Dial opens a client connection to t (it connects on first use).
// insecureTLS skips the certificate check for grpcs.
func Dial(t Target, insecureTLS bool, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	creds := grpcinsecure.NewCredentials()
	if t.TLS {
		host, _, _ := net.SplitHostPort(t.Addr)
		creds = credentials.NewTLS(&tls.Config{ServerName: host, InsecureSkipVerify: insecureTLS, MinVersion: tls.VersionTLS12}) //nolint:gosec // the user's http_tls_insecure setting
	}
	all := append([]grpc.DialOption{grpc.WithTransportCredentials(creds)}, opts...)
	cc, err := grpc.NewClient("passthrough:///"+t.Addr, all...)
	if err != nil {
		return nil, fmt.Errorf("grpc: %w", err)
	}
	return cc, nil
}
