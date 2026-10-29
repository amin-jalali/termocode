package ai

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// closedAddr is a local "host:port" where nothing listens.
func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

type fakeTimeout struct{}

func (fakeTimeout) Error() string   { return "i/o timeout" }
func (fakeTimeout) Timeout() bool   { return true }
func (fakeTimeout) Temporary() bool { return true }

// dialing is an HTTP client whose every connection fails with err.
func dialing(err error) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) { return nil, err },
	}}
}

// Spec (SCN-13): when the AI server can not be reached, the chat says so
// in plain words with the host and the likely cause, not as a Go error
// ("Post \"…\": dial tcp …: connect: connection refused").
func TestUnreachableAIServerMessage(t *testing.T) {
	addr := closedAddr(t)
	proxy := closedAddr(t)
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"refused, openai", Config{Kind: KindOpenAI, BaseURL: "http://" + addr + "/v1"},
			"No connection to the AI server (" + addr + "): it refused the connection. Is the server running?"},
		{"refused, anthropic", Config{Kind: KindAnthropic, BaseURL: "http://" + addr},
			"No connection to the AI server (" + addr + "): it refused the connection. Is the server running?"},
		{"proxy refused", Config{Kind: KindOpenAI, BaseURL: "https://api.example.test/v1", HTTPClient: &http.Client{Transport: &http.Transport{
			Proxy: func(*http.Request) (*url.URL, error) { return url.Parse("http://" + proxy) }}}},
			"No connection to the AI server (api.example.test): the proxy " + proxy + " refused the connection."},
		{"name not found", Config{Kind: KindOpenAI, BaseURL: "https://api.example.test/v1",
			HTTPClient: dialing(&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "api.example.test", IsNotFound: true}})},
			"No connection to the AI server (api.example.test): name not found. Are you offline?"},
		{"timeout", Config{Kind: KindOpenAI, BaseURL: "https://api.example.test/v1",
			HTTPClient: dialing(&net.OpError{Op: "dial", Net: "tcp", Err: fakeTimeout{}})},
			"No connection to the AI server (api.example.test): no answer in time. Are you offline?"},
	}
	for _, c := range cases {
		c.cfg.APIKey, c.cfg.Model = "sk-test-1234567890abcdef", "m1"
		_, last := Collect(NewProvider(c.cfg).Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}))
		if last.Err == nil || last.Err.Error() != c.want {
			t.Errorf("%s:\n got %v\nwant %s", c.name, last.Err, c.want)
		}
		if last.Err != nil && !IsUnreachable(last.Err.Error()) {
			t.Errorf("%s: IsUnreachable is false for %q", c.name, last.Err)
		}
	}

	// A stop by the user stays a cancel.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, last := Collect(NewProvider(Config{Kind: KindOpenAI, BaseURL: "http://" + addr + "/v1", Model: "m1", APIKey: "sk-test-1234567890abcdef"}).Stream(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}))
	if last.Err == nil || !strings.Contains(last.Err.Error(), context.Canceled.Error()) || IsUnreachable(last.Err.Error()) {
		t.Errorf("cancel: got %v", last.Err)
	}
	if IsUnreachable(errors.New("HTTP 500: busy").Error()) {
		t.Error("a server error is not unreachable")
	}
}
