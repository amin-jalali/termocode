package ai

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var errFirstByteTimeout = errors.New("the AI server did not start a reply in time; try again")

const (
	cloudFirstByte = 30 * time.Second
	localFirstByte = 3 * time.Minute // a local model may need time to load
)

// firstByteLimit is the silence allowed before a reply starts: longer
// for a local model (ollama, lmstudio, localhost or a private LAN
// address), which can take a while to load.
func firstByteLimit(kind, baseURL string) time.Duration {
	if kind == KindOllama || kind == KindLMStudio {
		return localFirstByte
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return cloudFirstByte
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return localFirstByte
	}
	if ip, err := netip.ParseAddr(host); err == nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
		return localFirstByte
	}
	return cloudFirstByte
}

// firstByteTransport limits silence, including a server that flushes
// headers but never sends a body. The client's longer limit still
// applies after the first body byte arrives.
type firstByteTransport struct {
	base  http.RoundTripper
	limit time.Duration       // 0 means 30 seconds
	arm   func(func()) func() // test clock; nil uses limit
}

func (t firstByteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancelCause(req.Context())
	arm := t.arm
	if arm == nil {
		limit := t.limit
		if limit == 0 {
			limit = cloudFirstByte
		}
		arm = func(f func()) func() { timer := time.AfterFunc(limit, f); return func() { timer.Stop() } }
	}
	stop := arm(func() { cancel(errFirstByteTimeout) })
	defer stop()
	resp, err := t.base.RoundTrip(req.Clone(ctx))
	if err != nil {
		cause := context.Cause(ctx)
		cancel(nil)
		if errors.Is(cause, errFirstByteTimeout) {
			return nil, cause
		}
		return nil, err
	}
	reader := bufio.NewReader(resp.Body)
	_, err = reader.Peek(1)
	if cause := context.Cause(ctx); errors.Is(cause, errFirstByteTimeout) {
		resp.Body.Close()
		cancel(nil)
		return nil, cause
	}
	if err != nil && err != io.EOF {
		resp.Body.Close()
		cancel(nil)
		return nil, err
	}
	resp.Body = &firstByteBody{Reader: reader, body: resp.Body, cancel: cancel}
	return resp, nil
}

type firstByteBody struct {
	*bufio.Reader
	body   io.ReadCloser
	cancel context.CancelCauseFunc
}

func (b *firstByteBody) Close() error { defer b.cancel(nil); return b.body.Close() }
