package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type firstByteRoundTripFunc func(*http.Request) (*http.Response, error)

func (f firstByteRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type firstByteBlockedBody struct {
	ctx     context.Context
	entered chan struct{}
}

func (b firstByteBlockedBody) Read([]byte) (int, error) {
	close(b.entered)
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (b firstByteBlockedBody) Close() error { return nil }

func TestFirstByteTimeout(t *testing.T) {
	for _, headers := range []bool{false, true} {
		t.Run(map[bool]string{false: "no headers", true: "headers only"}[headers], func(t *testing.T) {
			entered := make(chan struct{})
			armed := make(chan func(), 1)
			tr := firstByteTransport{
				base: firstByteRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					if headers {
						return &http.Response{StatusCode: 200, Body: firstByteBlockedBody{r.Context(), entered}, Header: make(http.Header)}, nil
					}
					close(entered)
					<-r.Context().Done()
					return nil, r.Context().Err()
				}),
				arm: func(f func()) func() { armed <- f; return func() {} },
			}
			done := make(chan error, 1)
			go func() {
				r, _ := http.NewRequest("GET", "http://example.test", nil)
				_, err := tr.RoundTrip(r)
				done <- err
			}()
			fire := <-armed
			<-entered
			fire()
			if err := <-done; !errors.Is(err, errFirstByteTimeout) || !strings.Contains(err.Error(), "did not start") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestFirstByteStopsOnlyShortTimer(t *testing.T) {
	stopped := false
	var requestContext context.Context
	tr := firstByteTransport{
		base: firstByteRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			requestContext = r.Context()
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("data: hello\n\n"))}, nil
		}),
		arm: func(func()) func() { return func() { stopped = true } },
	}
	req, _ := http.NewRequest("GET", "http://example.test", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !stopped || requestContext.Err() != nil {
		t.Fatal("first byte must stop the short timer without cancelling the stream")
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil || string(b) != "data: hello\n\n" {
		t.Fatalf("body %q: %v", b, err)
	}
}
