package app

import (
	"errors"
	"net/url"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/webtotp"
)

// The optional second factor of the web panel (WEB-48). The seed lives
// only in the secret store, as web.totp; this file is the one bridge
// between it and the web side (the hub's own web_*.go files never import
// the store). The gateway asks with web.totp_check and hears only yes or
// no; the seed and the code never go back to it.

const (
	confirmKindWebTotpShow    confirmKindEnum = 2610
	confirmKindWebTotpReplace confirmKindEnum = 2611
)

// webTotpSecretName is the seed's name in the secret store.
const webTotpSecretName = "web.totp"

// webTotpChecker remembers the last code taken and the tries, for the
// seed in the store (a new seed gets a new checker).
var webTotpChecker = &webtotp.Checker{}

// openWebTotpSetup makes a new seed (asking first when one exists) and
// shows it once.
func (m *Model) openWebTotpSetup() tea.Cmd {
	if m.confirmOpen {
		return m.webToast(toast.Warn, "Answer the open card first.")
	}
	return m.withSecretsUnlocked(func(m *Model) tea.Cmd {
		s, err := m.secretStore()
		if err != nil {
			return m.webToast(toast.Errr, "The secret store is not available.")
		}
		if _, ok, _ := s.Get(webTotpSecretName); ok {
			m.confirm = confirm.Risky("Replace the sign-in code?",
				"Your authenticator app's current code for termocode stops working.\nYou scan a new one next.",
				confirm.Button{ID: "keep", Title: "Keep"},
				confirm.Button{ID: "replace", Title: "Replace", Style: confirm.StyleDestructive})
			m.confirm.SetSize(m.w, m.h)
			m.confirmOpen = true
			m.confirmKind = confirmKindWebTotpReplace
			return nil
		}
		return m.webTotpNew()
	})
}

// webTotpNew stores a fresh seed and shows it.
func (m *Model) webTotpNew() tea.Cmd {
	s, err := m.secretStore()
	if err != nil {
		return m.webToast(toast.Errr, "The secret store is not available.")
	}
	_, b32, err := webtotp.NewSecret()
	if err != nil {
		return m.webToast(toast.Errr, "No sign-in code: no random numbers")
	}
	if err := s.Set(webTotpSecretName, b32); err != nil {
		return m.webToast(toast.Errr, "Could not save the sign-in code in the secret store.")
	}
	webTotpChecker = &webtotp.Checker{}
	account := "web"
	if st := m.webPanel; st != nil && st.url != "" {
		if u, err := url.Parse(st.url); err == nil {
			account = u.Hostname()
		}
	}
	var grouped []string
	for i := 0; i < len(b32); i += 4 {
		grouped = append(grouped, b32[i:min(i+4, len(b32))])
	}
	body := "Scan this with an authenticator app on your phone, or type the key.\n" +
		"It is shown only now; it is kept in your secret store.\n" +
		`Then set "web_require_totp": true in config.json.`
	m.confirm = confirm.New("Sign-in code for the web panel", body, []confirm.Button{{ID: "close", Title: "Close"}}).
		WithCode(strings.Join(grouped, " "), "").WithPicture(webQRLines(webtotp.URI(b32, account)))
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindWebTotpShow
	return nil
}

// handleWebTotpConfirm routes the TOTP cards. ok=false: not ours.
func (m *Model) handleWebTotpConfirm(id string) (tea.Cmd, bool) {
	switch m.confirmKind {
	case confirmKindWebTotpShow:
		return nil, true
	case confirmKindWebTotpReplace:
		if id == "replace" {
			return m.webTotpNew(), true
		}
		return nil, true
	}
	return nil, false
}

func init() {
	webOps[webproto.OpWebTotpCheck] = func(m *Model, call *webCall) tea.Cmd { return m.webTotpCheck(call) }
}

// webTotpCheck answers the gateway's web.totp_check: res for a right
// code, forbidden for anything else. The store is read off Update.
func (m *Model) webTotpCheck(call *webCall) tea.Cmd {
	args, _ := call.args.(*webproto.TotpCheckArgs)
	s, err := m.secretStore()
	if args == nil || err != nil || s == nil {
		call.fail(webproto.CodeForbidden, "No sign-in code is set up on the laptop.")
		return nil
	}
	if s.Locked() {
		call.fail(webproto.CodeForbidden, "Unlock the secret store on the laptop first.")
		return nil
	}
	checker := webTotpChecker
	call.later()
	crash.Go("web: a sign-in code", func() {
		b32, ok, err := s.Get(webTotpSecretName)
		if err != nil || !ok {
			call.fail(webproto.CodeForbidden, "No sign-in code is set up on the laptop.")
			return
		}
		seed, err := webtotp.Decode(b32)
		if err != nil {
			call.fail(webproto.CodeForbidden, "The sign-in code in the secret store is damaged.")
			return
		}
		switch err := checker.Check(seed, args.Code, time.Now()); {
		case err == nil:
			call.reply(nil)
		case errors.Is(err, webtotp.ErrTooMany):
			call.fail(webproto.CodeForbidden, "Too many tries. Wait a few minutes.")
		default:
			call.fail(webproto.CodeForbidden, "Wrong code.")
		}
	})
	return nil
}
