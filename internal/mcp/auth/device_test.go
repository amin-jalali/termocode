package auth

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

func deviceCfg(as *mcptest.AuthServer) DeviceConfig {
	return DeviceConfig{DeviceEndpoint: as.Issuer() + "/device/code", TokenEndpoint: as.Issuer() + "/token",
		Issuer: as.Issuer(), ClientID: "dev-app", Scopes: []string{"read:user", "repo"}, Resource: "https://mcp.example/mcp/"}
}

// fakeSleep records the waits and returns at once.
type fakeSleep struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (f *fakeSleep) sleep(ctx context.Context, d time.Duration) error {
	f.mu.Lock()
	f.waits = append(f.waits, d)
	f.mu.Unlock()
	return ctx.Err()
}

// Spec (CX-1): BeginDevice returns the user code and the verification
// URI from the server; the device code is a secret (Redact hides it).
func TestBeginDevice(t *testing.T) {
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{Device: true, PreClientID: "dev-app"})
	d, err := BeginDevice(ctx(t), deviceCfg(as))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(d.UserCode(), "WDJB-") || d.VerificationURI() != as.Issuer()+"/device" ||
		!strings.Contains(d.VerificationURIComplete(), d.UserCode()) || d.Interval() != time.Second {
		t.Errorf("flow %q %q %q %v", d.UserCode(), d.VerificationURI(), d.VerificationURIComplete(), d.Interval())
	}
	if time.Until(d.ExpiresAt()) < 14*time.Minute {
		t.Errorf("expires %v", d.ExpiresAt())
	}
	if strings.Contains(ai.Redact("code "+d.deviceCode), d.deviceCode) {
		t.Error("the device code is not registered as a secret")
	}
}

// Spec: Poll waits interval before each poll, adds 5 s on slow_down and
// returns the tokens bound to the resource.
func TestDevicePollIntervalAndSlowDown(t *testing.T) {
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{Device: true, PreClientID: "dev-app", PendingPolls: 2, SlowDown: true, DeviceInterval: 3})
	d, err := BeginDevice(ctx(t), deviceCfg(as))
	if err != nil {
		t.Fatal(err)
	}
	fs := &fakeSleep{}
	d.sleep = fs.sleep
	tok, err := d.Poll(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{3 * time.Second, 8 * time.Second, 8 * time.Second, 8 * time.Second}
	if len(fs.waits) != len(want) {
		t.Fatalf("waits %v, want %v", fs.waits, want)
	}
	for i := range want {
		if fs.waits[i] != want[i] {
			t.Fatalf("waits %v, want %v", fs.waits, want)
		}
	}
	if !as.Valid(tok.AccessToken) || tok.RefreshToken == "" || tok.Resource != "https://mcp.example/mcp/" ||
		tok.Issuer != as.Issuer() || tok.TokenEndpoint != as.Issuer()+"/token" || tok.ClientID != "dev-app" {
		t.Errorf("token %+v", tok)
	}
	if strings.Contains(ai.Redact(tok.AccessToken), tok.AccessToken) {
		t.Error("access token not registered")
	}
	// The AS gave a refresh token: Refresh works afterwards.
	nt, err := Refresh(ctx(t), nil, tok)
	if err != nil || nt.AccessToken == tok.AccessToken || !as.Valid(nt.AccessToken) {
		t.Errorf("refresh: %v %+v", err, nt)
	}
}

// Spec: access_denied and expired_token end the poll with typed
// errors, also when the server answers errors with HTTP 200 (github.com).
func TestDevicePollErrors(t *testing.T) {
	for _, c := range []struct {
		mode   string
		gh     bool
		target error
	}{{"denied", false, ErrDeviceDenied}, {"expired", false, ErrDeviceExpired}, {"denied", true, ErrDeviceDenied}, {"expired", true, ErrDeviceExpired}} {
		as := mcptest.NewAuthServer(t, mcptest.AuthOptions{Device: true, PreClientID: "dev-app", DeviceMode: c.mode, GitHubStyle: c.gh})
		d, err := BeginDevice(ctx(t), deviceCfg(as))
		if err != nil {
			t.Fatal(err)
		}
		d.sleep = (&fakeSleep{}).sleep
		if _, err := d.Poll(ctx(t)); !errors.Is(err, c.target) {
			t.Errorf("%s (github %v): %v", c.mode, c.gh, err)
		}
	}
}

// Spec: past expires_in the poll stops on its own.
func TestDevicePollLocalExpiry(t *testing.T) {
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{Device: true, PreClientID: "dev-app", DeviceMode: "manual"})
	d, err := BeginDevice(ctx(t), deviceCfg(as))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	d.now = func() time.Time { return now }
	d.sleep = func(ctx context.Context, w time.Duration) error { now = now.Add(w); return nil }
	if _, err := d.Poll(ctx(t)); !errors.Is(err, ErrDeviceExpired) {
		t.Errorf("err %v", err)
	}
	if n := as.DevicePolls(); n < 100 || n > 1000 {
		t.Errorf("polls before the 15-minute expiry: %d", n)
	}
}

// Spec: a cancelled context ends Poll in < 100 ms and leaves no
// goroutine behind.
func TestDevicePollCancel(t *testing.T) {
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{Device: true, PreClientID: "dev-app", DeviceMode: "manual", DeviceInterval: 30})
	d, err := BeginDevice(ctx(t), deviceCfg(as))
	if err != nil {
		t.Fatal(err)
	}
	before := runtime.NumGoroutine()
	c, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := d.Poll(c); done <- err }()
	time.Sleep(20 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err %v", err)
		}
		if el := time.Since(start); el > 100*time.Millisecond {
			t.Errorf("Poll took %v to stop", el)
		}
	case <-time.After(time.Second):
		t.Fatal("Poll did not stop")
	}
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("goroutines %d > %d after cancel", n, before)
	}
}

// Spec: a manual approval (the user typed the code on the web page)
// ends the poll with tokens; no refresh token from a GitHub OAuth app.
func TestDevicePollManualApproval(t *testing.T) {
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{Device: true, PreClientID: "dev-app", DeviceMode: "manual", NoRefresh: true, GitHubStyle: true})
	d, err := BeginDevice(ctx(t), deviceCfg(as))
	if err != nil {
		t.Fatal(err)
	}
	polls := 0
	d.sleep = func(ctx context.Context, _ time.Duration) error {
		polls++
		if polls == 3 {
			as.ApproveDevice(d.UserCode())
		}
		return ctx.Err()
	}
	tok, err := d.Poll(ctx(t))
	if err != nil || tok.RefreshToken != "" || !as.Valid(tok.AccessToken) {
		t.Errorf("%v %+v", err, tok)
	}
}

// Spec: a bad device endpoint (not https off this machine) is refused,
// and an unknown client is a clear error.
func TestBeginDeviceRefuses(t *testing.T) {
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{Device: true, PreClientID: "dev-app"})
	c := deviceCfg(as)
	c.DeviceEndpoint = "http://example.com/device"
	if _, err := BeginDevice(ctx(t), c); err == nil {
		t.Error("http endpoint accepted")
	}
	c = deviceCfg(as)
	c.ClientID = "nobody"
	if _, err := BeginDevice(ctx(t), c); err == nil || !strings.Contains(err.Error(), "invalid_client") {
		t.Errorf("unknown client: %v", err)
	}
}
