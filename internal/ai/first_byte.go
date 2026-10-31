package ai

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

var errFirstByteTimeout = errors.New("the AI server did not start a reply within 30 seconds; try again")

// firstByteTransport limits silence, including a server that flushes
// headers but never sends a body. The client's longer limit still
// applies after the first body byte arrives.
type firstByteTransport struct {
	base http.RoundTripper
	arm  func(func()) func() // test clock; nil uses the 30-second limit
}

func (t firstByteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancelCause(req.Context())
	arm := t.arm
	if arm == nil {
		arm = func(f func()) func() { timer := time.AfterFunc(30*time.Second, f); return func() { timer.Stop() } }
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
