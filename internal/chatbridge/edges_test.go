package chatbridge

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAPIErrorText(t *testing.T) {
	if s := (&APIError{Status: 500}).Error(); s != "HTTP 500" {
		t.Fatal(s)
	}
	if s := (&APIError{Status: 400, Msg: "bad"}).Error(); s != "HTTP 400: bad" {
		t.Fatal(s)
	}
	if IsUnauthorized(errors.New("x")) || IsUnauthorized(&APIError{Status: 403}) {
		t.Fatal("only 401 is a bad token")
	}
}

func TestTelegramRateLimitAndNotOK(t *testing.T) {
	n := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"ok":false,"description":"Too Many Requests","parameters":{"retry_after":7}}`)
			return
		}
		if n == 2 {
			_, _ = io.WriteString(w, `{"ok":false,"description":"odd"}`)
			return
		}
		_, _ = io.WriteString(w, `not json`)
	}))
	defer s.Close()
	tg := &Telegram{Token: "t", Base: s.URL}
	_, err := tg.Poll(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) || ae.RetryAfter != 7*time.Second {
		t.Fatalf("429: %v", err)
	}
	if _, err := tg.Poll(context.Background()); err == nil || !strings.Contains(err.Error(), "odd") {
		t.Fatalf("ok false: %v", err)
	}
	if _, err := tg.Poll(context.Background()); err == nil {
		t.Fatal("bad JSON passed")
	}
	if tg.Name() != "telegram" || (&Discord{}).Name() != "discord" {
		t.Fatal("names")
	}
}

func TestDiscordRateLimitAndChannelErrors(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/@me" {
			_, _ = io.WriteString(w, `{"id":"1"}`)
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"message":"You are being rate limited.","retry_after":1.5}`)
	}))
	defer s.Close()
	d := &Discord{Token: "t", Base: s.URL, Channels: []string{"c"}}
	err := d.Start(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) || ae.RetryAfter != 1500*time.Millisecond {
		t.Fatalf("429: %v", err)
	}
	d.after = map[string]string{"c": "0"}
	if _, err := d.Poll(context.Background()); err == nil {
		t.Fatal("poll error lost")
	}
	if err := d.Send(context.Background(), "c", "x"); err == nil {
		t.Fatal("send error lost")
	}
}

func TestCmpSnowflake(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"9", "10", -1}, {"10", "9", 1}, {"5", "5", 0}, {"99999999999999999999", "100000000000000000000", -1}, {"x", "y", -1}} {
		got := cmpSnowflake(c.a, c.b)
		if (got < 0) != (c.want < 0) || (got > 0) != (c.want > 0) {
			t.Fatalf("%s vs %s: %d", c.a, c.b, got)
		}
	}
}

type failSend struct{ fakeConn }

func (f *failSend) Send(context.Context, string, string) error {
	return errors.New("blocked by the user")
}

func TestBridgeLogsSendErrorsAndStartErrors(t *testing.T) {
	var logs []string
	f := &failSend{fakeConn{batches: [][]Message{{{Chat: "c", User: "7", Text: "hi"}}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f.cancel = cancel
	b := &Bridge{Conn: f, Allow: []string{"7"}, Pause: time.Nanosecond, Log: func(s string) { logs = append(logs, s) },
		Handle: func(context.Context, Message) (string, error) { return "ok", nil }}
	if err := b.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "blocked by the user") {
		t.Fatalf("logs: %q", logs)
	}
	bad := &fakeConn{startErr: errors.New("no route")}
	if err := (&Bridge{Conn: bad, Allow: []string{"7"}, Handle: b.Handle}).Run(context.Background()); err == nil || !strings.Contains(err.Error(), "no route") {
		t.Fatalf("start error: %v", err)
	}
	if err := (&Bridge{Allow: []string{"7"}}).Run(context.Background()); err == nil {
		t.Fatal("no handler, but ran")
	}
}
