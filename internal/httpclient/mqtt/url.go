package mqtt

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Target is a broker read from an mqtt:// or mqtts:// URL.
type Target struct {
	Addr     string // host:port
	Host     string // for the TLS server name
	Topic    string // the URL path without its leading "/" ("" = none)
	Username string
	Password string
	TLS      bool
}

// ParseURL reads mqtt://[user[:pass]@]host[:port][/topic] (port 1883)
// or mqtts://… (TLS, port 8883).
func ParseURL(raw string) (Target, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Target{}, fmt.Errorf("mqtt: bad URL: %v", err)
	}
	var t Target
	port := "1883"
	switch strings.ToLower(u.Scheme) {
	case "mqtt", "tcp":
	case "mqtts", "ssl", "tls":
		t.TLS, port = true, "8883"
	default:
		return Target{}, fmt.Errorf("mqtt: the URL must start with mqtt:// or mqtts://")
	}
	t.Host = u.Hostname()
	if t.Host == "" {
		return Target{}, fmt.Errorf("mqtt: the URL has no host")
	}
	if p := u.Port(); p != "" {
		port = p
	}
	t.Addr = net.JoinHostPort(t.Host, port)
	t.Topic = strings.TrimPrefix(u.Path, "/")
	if u.User != nil {
		t.Username = u.User.Username()
		t.Password, _ = u.User.Password()
	}
	return t, nil
}

// String is the broker without user info, safe to show.
func (t Target) String() string {
	s := "mqtt://"
	if t.TLS {
		s = "mqtts://"
	}
	return s + t.Addr
}

// DialTarget connects to t, over TLS for mqtts (insecure skips the
// certificate check). User info in the URL fills empty Options.
func DialTarget(ctx context.Context, t Target, o Options, insecure bool) (*Client, error) {
	if o.Username == "" {
		o.Username, o.Password = t.Username, t.Password
	}
	if !t.TLS {
		return Dial(ctx, t.Addr, o)
	}
	d := tls.Dialer{Config: &tls.Config{ServerName: t.Host, InsecureSkipVerify: insecure, MinVersion: tls.VersionTLS12}} //nolint:gosec // insecure only on the user's choice
	conn, err := d.DialContext(ctx, "tcp", t.Addr)
	if err != nil {
		return nil, err
	}
	c, err := NewClient(ctx, conn, o)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}
