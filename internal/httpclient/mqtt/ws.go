package mqtt

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/coder/websocket"
)

// wsReadLimit caps one WebSocket frame (an MQTT packet may span frames).
const wsReadLimit = 16 << 20

func (t Target) wsScheme() string {
	if t.TLS {
		return "wss"
	}
	return "ws"
}

// wsURL is the WebSocket endpoint of t, with its query.
func (t Target) wsURL() string {
	u := t.wsScheme() + "://" + t.Addr + t.Path
	if t.RawQuery != "" {
		u += "?" + t.RawQuery
	}
	return u
}

// dialWS connects to t over a WebSocket with the "mqtt" subprotocol
// and runs MQTT over its binary frames.
func dialWS(ctx context.Context, t Target, o Options, insecure bool) (*Client, error) {
	tr := &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{ServerName: t.Host, InsecureSkipVerify: insecure, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}, //nolint:gosec // insecure only on the user's choice
	}
	ws, resp, err := websocket.Dial(ctx, t.wsURL(), &websocket.DialOptions{
		HTTPClient:   &http.Client{Transport: tr},
		Subprotocols: []string{"mqtt"},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		tr.CloseIdleConnections()
		return nil, fmt.Errorf("mqtt: websocket: %w", err)
	}
	ws.SetReadLimit(wsReadLimit)
	conn := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
	c, err := NewClient(ctx, conn, o)
	if err != nil {
		_ = conn.Close()
		tr.CloseIdleConnections()
		return nil, err
	}
	return c, nil
}
