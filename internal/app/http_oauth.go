package app

// OAuth 2.0 before a send (API-6): a request with `# @oauth2 …` gets a
// token first. Tokens are cached in the secret store
// (http.oauth2.<hash>) and refreshed with their refresh token. The
// authorization code grant opens the browser (loopback redirect, PKCE);
// the device code grant shows the user code on a card. Cancel is the
// first button on both cards and stops the wait.

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/toast"
)

const (
	confirmKindHTTPOAuthDevice  confirmKindEnum = 4513
	confirmKindHTTPOAuthBrowser confirmKindEnum = 4514
)

// httpOAuthStore is where tokens are cached (a test hook).
var httpOAuthStore = func() secrets.Store {
	s, _ := configureSecretStore()
	return s
}

// httpOAuthWait is a run that waits for its token.
type httpOAuthWait struct {
	run    *httpRun
	hooks  httpclient.Hooks
	cfg    httpclient.OAuth2Config
	cancel context.CancelFunc
	url    string // the page the user opens (browser / device)
}

type httpOAuthDoneMsg struct {
	w   *httpOAuthWait
	tok httpclient.OAuth2Token
	err error
}

type httpOAuthDeviceMsg struct {
	w   *httpOAuthWait
	ctx context.Context
	d   httpclient.OAuth2Device
	err error
}

func httpOAuthClient() *http.Client { return &http.Client{Timeout: 30 * time.Second} }

// httpOAuthCached reads a cached token (zero when none or locked).
func httpOAuthCached(key string) httpclient.OAuth2Token {
	st := httpOAuthStore()
	if st == nil || st.Locked() {
		return httpclient.OAuth2Token{}
	}
	v, ok, err := st.Get(key)
	if err != nil || !ok {
		return httpclient.OAuth2Token{}
	}
	var t httpclient.OAuth2Token
	if json.Unmarshal([]byte(v), &t) != nil {
		return httpclient.OAuth2Token{}
	}
	return t
}

func httpOAuthSave(key string, t httpclient.OAuth2Token) error {
	st := httpOAuthStore()
	if st == nil || st.Locked() {
		return nil // no cache: the next send asks again
	}
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return st.Set(key, string(b))
}

// httpOAuthApply puts the token on the run.
func httpOAuthApply(run *httpRun, t httpclient.OAuth2Token) {
	ai.RegisterSecret(t.AccessToken)
	run.rr.Headers = append(slices.Clip(run.rr.Headers), httpclient.Header{Name: "Authorization", Value: "Bearer " + t.AccessToken})
	run.rr.Secrets = append(slices.Clip(run.rr.Secrets), t.AccessToken)
}

// httpOAuthGate runs before a send. wait=true: the send goes on later
// (cmd gets the token) or not at all (cmd is a toast).
func (m *Model) httpOAuthGate(run *httpRun, hooks httpclient.Hooks) (tea.Cmd, bool) {
	spec, ok := run.rr.Directives[httpclient.OAuth2Directive]
	if !ok || run.rr.HasHeader("Authorization") {
		return nil, false
	}
	cfg, err := httpclient.ParseOAuth2(spec)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "OAuth 2.0: request not sent", ai.Redact(err.Error())), true
	}
	now := time.Now()
	cached := httpOAuthCached(cfg.CacheKey())
	if cached.Valid(now) {
		httpOAuthApply(run, cached)
		return nil, false
	}
	s := m.httpS()
	if s.oauthWait != nil && s.oauthWait.cancel != nil {
		s.oauthWait.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &httpOAuthWait{run: run, hooks: hooks, cfg: cfg, cancel: cancel}
	s.oauthWait = w
	hc := httpOAuthClient()
	if cached.RefreshToken != "" {
		return func() tea.Msg {
			t, err := httpclient.OAuth2Refresh(ctx, hc, cfg, cached.RefreshToken, time.Now())
			if err != nil && cfg.Grant != httpclient.GrantAuthCode && cfg.Grant != httpclient.GrantDeviceCode {
				t, err = httpclient.OAuth2Fetch(ctx, hc, cfg, time.Now())
			}
			return httpOAuthDoneMsg{w: w, tok: t, err: err}
		}, true
	}
	switch cfg.Grant {
	case httpclient.GrantAuthCode:
		lb, err := httpclient.NewOAuth2Loopback()
		if err != nil {
			cancel()
			s.oauthWait = nil
			return m.pushToastDetail(toast.Errr, "OAuth 2.0 sign-in", err.Error()), true
		}
		verifier, challenge := httpclient.NewPKCE()
		w.url = httpclient.OAuth2AuthCodeURL(cfg, lb.RedirectURI, lb.State, challenge)
		m.httpOAuthBrowserCard(w)
		open := m.openLink(w.url)
		wait := func() tea.Msg {
			defer lb.Close()
			code, err := lb.Wait(ctx)
			if err != nil {
				return httpOAuthDoneMsg{w: w, err: err}
			}
			t, err := httpclient.OAuth2ExchangeCode(ctx, hc, cfg, code, lb.RedirectURI, verifier, time.Now())
			return httpOAuthDoneMsg{w: w, tok: t, err: err}
		}
		return tea.Batch(open, wait), true
	case httpclient.GrantDeviceCode:
		return func() tea.Msg {
			d, err := httpclient.OAuth2DeviceStart(ctx, hc, cfg)
			return httpOAuthDeviceMsg{w: w, ctx: ctx, d: d, err: err}
		}, true
	}
	return func() tea.Msg {
		t, err := httpclient.OAuth2Fetch(ctx, hc, cfg, time.Now())
		return httpOAuthDoneMsg{w: w, tok: t, err: err}
	}, true
}

func (m *Model) httpOAuthBrowserCard(w *httpOAuthWait) {
	m.confirm = confirm.New("Sign in with the browser",
		"Finish the sign-in in the browser page that opened. termocode sends the request when it comes back.",
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "open", Title: "Open page again", Style: confirm.StylePrimary}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindHTTPOAuthBrowser
}

// httpOAuthDeviceCard shows the device code to type on the page.
func (m *Model) httpOAuthDeviceCard(w *httpOAuthWait, code, page string) {
	w.url = page
	m.confirm = confirm.New("Sign in: enter "+code,
		"Open "+page+" on any device and enter the code "+code+". termocode sends the request when you approved it.",
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "open", Title: "Open page", Style: confirm.StylePrimary}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindHTTPOAuthDevice
}

// httpOAuthConfirm answers the two cards. ok=false: not ours.
func (m *Model) httpOAuthConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindHTTPOAuthDevice && m.confirmKind != confirmKindHTTPOAuthBrowser {
		return nil, false
	}
	s := m.httpS()
	w := s.oauthWait
	if w == nil {
		return nil, true
	}
	if id == "open" {
		return m.openLink(w.url), true
	}
	if w.cancel != nil {
		w.cancel()
	}
	s.oauthWait = nil
	return nil, true
}

// handleHTTPOAuthMsg handles the token messages. ok=false: not ours.
func (m *Model) handleHTTPOAuthMsg(msg tea.Msg) (tea.Cmd, bool) {
	s := m.httpS()
	switch msg := msg.(type) {
	case httpOAuthDeviceMsg:
		if s.oauthWait != msg.w {
			return nil, true
		}
		if msg.err != nil {
			s.oauthWait = nil
			return m.pushToastDetail(toast.Warn, "OAuth 2.0: request not sent", ai.Redact(msg.err.Error())), true
		}
		page := msg.d.VerificationURI
		if msg.d.VerificationURIComplete != "" {
			page = msg.d.VerificationURIComplete
		}
		m.httpOAuthDeviceCard(msg.w, msg.d.UserCode, page)
		w, ctx, d, cfg := msg.w, msg.ctx, msg.d, msg.w.cfg
		return func() tea.Msg {
			t, err := httpclient.OAuth2DevicePoll(ctx, httpOAuthClient(), cfg, d, httpclient.SleepCtx, time.Now())
			return httpOAuthDoneMsg{w: w, tok: t, err: err}
		}, true
	case httpOAuthDoneMsg:
		if s.oauthWait != msg.w {
			return nil, true // cancelled or replaced
		}
		s.oauthWait = nil
		if m.confirmOpen && (m.confirmKind == confirmKindHTTPOAuthDevice || m.confirmKind == confirmKindHTTPOAuthBrowser) {
			m.confirmOpen = false
		}
		if msg.w.cancel != nil {
			msg.w.cancel()
		}
		if msg.err != nil {
			return m.pushToastDetail(toast.Warn, "OAuth 2.0: request not sent", ai.Redact(msg.err.Error())), true
		}
		var warn tea.Cmd
		if err := httpOAuthSave(msg.w.cfg.CacheKey(), msg.tok); err != nil {
			warn = m.pushToastDetail(toast.Warn, "OAuth token not cached", err.Error())
		}
		httpOAuthApply(msg.w.run, msg.tok)
		return tea.Batch(warn, m.startHTTPRun(msg.w.run, msg.w.hooks)), true
	}
	return nil, false
}
