package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The device authorization grant (RFC 8628): termocode shows a short
// code, the user types it on the service's web page (on any device, so it
// is the same on a laptop and over SSH), and termocode polls the token
// endpoint until the user said yes. GitHub (termocode's own OAuth app)
// and GitLab (with an app id) offer it. No client secret is needed.

// ErrDeviceDenied: the user pressed Cancel on the web page.
var ErrDeviceDenied = errors.New("the sign-in was cancelled on the web page: nothing was connected")

// ErrDeviceExpired: the code ended before the user approved it.
var ErrDeviceExpired = errors.New("the code expired before the sign-in was approved")

// deviceGrant is the grant_type of the poll.
const deviceGrant = "urn:ietf:params:oauth:grant-type:device_code"

// slowDownStep is what slow_down adds to the interval (RFC 8628 §3.5).
const slowDownStep = 5 * time.Second

// DeviceConfig starts one device sign-in.
type DeviceConfig struct {
	DeviceEndpoint string // device_authorization_endpoint
	TokenEndpoint  string
	Issuer         string // stored with the tokens
	ClientID       string // a registered public client (no secret)
	Scopes         []string
	// Resource binds the tokens to the MCP server (RFC 8707); it is sent
	// with the poll unless NoResourceParam (servers that do not take it).
	Resource        string
	NoResourceParam bool
	HTTP            *http.Client
}

// DeviceFlow is one pending device sign-in.
type DeviceFlow struct {
	cfg         DeviceConfig
	deviceCode  string
	userCode    string
	uri         string
	uriComplete string
	interval    time.Duration
	expires     time.Time

	// sleep and now are the clock (tests replace them).
	sleep func(ctx context.Context, d time.Duration) error
	now   func() time.Time
}

// UserCode is the code the user types on the web page (not a secret).
func (d *DeviceFlow) UserCode() string { return d.userCode }

// VerificationURI is the page where the user types the code. It comes
// from the server, never from what a user typed.
func (d *DeviceFlow) VerificationURI() string { return d.uri }

// VerificationURIComplete is the page with the code filled in ("" when
// the server has none: GitHub).
func (d *DeviceFlow) VerificationURIComplete() string { return d.uriComplete }

// Interval is the current wait between two polls.
func (d *DeviceFlow) Interval() time.Duration { return d.interval }

// ExpiresAt is when the code ends.
func (d *DeviceFlow) ExpiresAt() time.Time { return d.expires }

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// BeginDevice asks the server for a device code and a user code.
func BeginDevice(ctx context.Context, cfg DeviceConfig) (*DeviceFlow, error) {
	for _, e := range []string{cfg.DeviceEndpoint, cfg.TokenEndpoint} {
		if err := checkEndpoint(e); err != nil {
			return nil, err
		}
	}
	if cfg.ClientID == "" {
		return nil, errors.New("the device sign-in needs a client id")
	}
	form := url.Values{"client_id": {cfg.ClientID}}
	if len(cfg.Scopes) > 0 {
		form.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.DeviceEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client(cfg.HTTP).Do(req)
	if err != nil {
		return nil, fmt.Errorf("device sign-in: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var r struct {
		DeviceCode  string `json:"device_code"`
		UserCode    string `json:"user_code"`
		URI         string `json:"verification_uri"`
		URL         string `json:"verification_url"` // older servers
		URIComplete string `json:"verification_uri_complete"`
		Interval    int    `json:"interval"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
	}
	_ = json.Unmarshal(b, &r)
	if resp.StatusCode != http.StatusOK || r.Error != "" {
		return nil, fmt.Errorf("device sign-in: HTTP %d: %s", resp.StatusCode, ai.Redact(oauthErrText(b)))
	}
	if r.URI == "" {
		r.URI = r.URL
	}
	if r.DeviceCode == "" || r.UserCode == "" || r.URI == "" {
		return nil, errors.New("device sign-in: the answer has no code or page")
	}
	if err := checkEndpoint(r.URI); err != nil {
		return nil, err
	}
	if r.URIComplete != "" && checkEndpoint(r.URIComplete) != nil {
		r.URIComplete = ""
	}
	ai.RegisterSecret(r.DeviceCode)
	d := &DeviceFlow{cfg: cfg, deviceCode: r.DeviceCode, userCode: r.UserCode, uri: r.URI, uriComplete: r.URIComplete,
		sleep: sleepCtx, now: time.Now}
	d.interval = time.Duration(r.Interval) * time.Second
	if d.interval <= 0 {
		d.interval = 5 * time.Second // RFC 8628 default
	}
	exp := time.Duration(r.ExpiresIn) * time.Second
	if exp <= 0 {
		exp = 15 * time.Minute
	}
	d.expires = d.now().Add(exp)
	return d, nil
}

// Poll waits for the user's answer: it polls the token endpoint every
// Interval (5 s more after each slow_down) until tokens come, the user
// cancels (ErrDeviceDenied), the code ends (ErrDeviceExpired) or ctx
// ends. It runs on the caller's goroutine and starts none.
func (d *DeviceFlow) Poll(ctx context.Context) (ai.MCPOAuth, error) {
	e := ai.MCPOAuth{Issuer: d.cfg.Issuer, Resource: d.cfg.Resource, TokenEndpoint: d.cfg.TokenEndpoint,
		ClientID: d.cfg.ClientID, Scope: strings.Join(d.cfg.Scopes, " ")}
	for {
		if err := d.sleep(ctx, d.interval); err != nil {
			return e, err
		}
		if !d.now().Before(d.expires) {
			return e, ErrDeviceExpired
		}
		form := url.Values{"grant_type": {deviceGrant}, "device_code": {d.deviceCode}}
		if d.cfg.Resource != "" && !d.cfg.NoResourceParam {
			form.Set("resource", d.cfg.Resource)
		}
		status, b, err := postToken(ctx, d.cfg.HTTP, e, form)
		if err != nil {
			if ctx.Err() != nil {
				return e, ctx.Err()
			}
			return e, err
		}
		var oe struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &oe)
		switch oe.Error {
		case "":
			if status != http.StatusOK {
				return e, fmt.Errorf("device sign-in: HTTP %d: %s", status, ai.Redact(oauthErrText(b)))
			}
			return tokenAnswer(e, b)
		case "authorization_pending":
		case "slow_down":
			d.interval += slowDownStep
		case "access_denied":
			return e, ErrDeviceDenied
		case "expired_token":
			return e, ErrDeviceExpired
		default:
			return e, fmt.Errorf("device sign-in: %s", ai.Redact(oauthErrText(b)))
		}
	}
}
