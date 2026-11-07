package mqtt

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Target is a broker read from an mqtt://, mqtts://, ws:// or wss:// URL.
type Target struct {
	Addr     string // host:port
	Host     string // for the TLS server name
	Topic    string // mqtt(s): the URL path without its leading "/"; ws(s): the #fragment ("" = none)
	Username string
	Password string
	TLS      bool
	// WS is MQTT over WebSocket (ws:// or wss://): Path and RawQuery
	// are the WebSocket endpoint (default path /mqtt).
	WS       bool
	Path     string
	RawQuery string
}

// ParseURL reads mqtt://[user[:pass]@]host[:port][/topic] (port 1883),
// mqtts://… (TLS, port 8883), or MQTT over WebSocket:
// ws://[user[:pass]@]host[:port][/path][?query][#topic] (port 80, path
// /mqtt) and wss://… (TLS, port 443).
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
	case "ws":
		t.WS, port = true, "80"
	case "wss":
		t.WS, t.TLS, port = true, true, "443"
	default:
		return Target{}, fmt.Errorf("mqtt: the URL must start with mqtt://, mqtts://, ws:// or wss://")
	}
	t.Host = u.Hostname()
	if t.Host == "" {
		return Target{}, fmt.Errorf("mqtt: the URL has no host")
	}
	if p := u.Port(); p != "" {
		port = p
	}
	t.Addr = net.JoinHostPort(t.Host, port)
	if t.WS {
		t.Path, t.RawQuery, t.Topic = u.EscapedPath(), u.RawQuery, u.Fragment
		if t.Path == "" {
			t.Path = "/mqtt"
		}
	} else {
		t.Topic = strings.TrimPrefix(u.Path, "/")
	}
	if u.User != nil {
		t.Username = u.User.Username()
		t.Password, _ = u.User.Password()
	}
	return t, nil
}

// String is the broker without user info or query, safe to show.
func (t Target) String() string {
	if t.WS {
		return t.wsScheme() + "://" + t.Addr + t.Path
	}
	s := "mqtt://"
	if t.TLS {
		s = "mqtts://"
	}
	return s + t.Addr
}

// DialTarget connects to t: TCP, TLS for mqtts, WebSocket for ws and
// wss (insecure skips the certificate check). User info in the URL
// fills empty Options.
func DialTarget(ctx context.Context, t Target, o Options, insecure bool) (*Client, error) {
	if o.Username == "" {
		o.Username, o.Password = t.Username, t.Password
	}
	if t.WS {
		return dialWS(ctx, t, o, insecure)
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
