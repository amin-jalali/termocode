package chatbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxBody caps what is read from one reply.
const maxBody = 4 << 20

// call does one JSON request: body (nil: none) as JSON, the reply
// decoded into out. secret is cut from every error (a Telegram token is
// in the URL). fail reads the service's error message from a non-2xx
// reply.
func call(ctx context.Context, c *http.Client, method, url string, hdr http.Header, body any, out any, secret string,
	fail func(status int, b []byte, h http.Header) *APIError) error {
	if c == nil {
		c = &http.Client{Timeout: 90 * time.Second}
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return hide(err, secret)
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		return hide(err, secret)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return hide(err, secret)
	}
	if resp.StatusCode/100 != 2 {
		ae := fail(resp.StatusCode, b, resp.Header)
		ae.Msg = strings.ReplaceAll(ae.Msg, secret, "<token>")
		return ae
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return errors.New("the reply is not the JSON expected")
	}
	return nil
}

// hide is err with secret cut out of its text.
func hide(err error, secret string) error {
	if secret == "" || !strings.Contains(err.Error(), secret) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), secret, "<token>"))
}
