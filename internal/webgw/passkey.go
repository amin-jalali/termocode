//go:build unix

package webgw

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/amin-jalali/termocode/internal/webdevices"
)

// Passkeys (WEB-33, web design 3.3): a paired device signs in again with
// the passkey it made while pairing. No passwords anywhere. The relying
// party is the panel's own origin (the tailnet name, or localhost for a
// local panel; an IP address cannot be one). Every key must be
// discoverable and every use needs user verification (the phone's Face
// ID, fingerprint or PIN), so a lost phone alone is not enough (T30).
// The library checks the origin, the RP id hash, the challenge, the
// flags and the signature; this file adds one-use challenges with a
// short life, the device store, and the clone check of the sign count.

// LoginTTL is how long a login challenge may be answered.
const LoginTTL = 60 * time.Second

// RegisterTTL is how long a pairing's passkey may take to be made.
const RegisterTTL = PairTTL

// maxLogins caps the login challenges open at once.
const maxLogins = 16

// Passkey errors. They say what happened, never a key or a challenge.
var (
	ErrPasskey        = errors.New("the passkey was not accepted")
	ErrLoginExpired   = errors.New("this sign-in expired or was used; start again")
	ErrCloneSuspected = errors.New("this passkey looks copied; the device must pair again")
	ErrDeviceRemoved  = errors.New("this device is not paired (any more); pair it again")
	ErrTooManyLogins  = errors.New("too many sign-ins at once; try again in a minute")
)

// Passkeys runs registrations and logins for one site.
type Passkeys struct {
	wa     *webauthn.WebAuthn
	rpID   string
	origin string
	store  *webdevices.Store
	now    func() time.Time

	mu     sync.Mutex
	logins map[string]*loginSess
}

type loginSess struct {
	data  webauthn.SessionData
	until time.Time
}

// passkeyReg is one registration in progress (held by its pairing).
type passkeyReg struct {
	user  *passkeyUser
	data  webauthn.SessionData
	until time.Time
}

// NewPasskeys makes the passkey side of site: a tailnet site, or a
// loopback site as localhost.
func NewPasskeys(site Site, store *webdevices.Store, now func() time.Time) (*Passkeys, error) {
	if now == nil {
		now = time.Now
	}
	rpID, origin := "", ""
	for _, h := range site.Hosts {
		name := h
		if host, _, err := net.SplitHostPort(h); err == nil {
			name = host
		}
		if net.ParseIP(name) == nil {
			rpID, origin = name, site.Scheme+"://"+h
			break
		}
	}
	if rpID == "" {
		return nil, errors.New("no host name for passkeys")
	}
	sel := protocol.AuthenticatorSelection{
		ResidentKey:        protocol.ResidentKeyRequirementRequired,
		RequireResidentKey: protocol.ResidentKeyRequired(),
		UserVerification:   protocol.VerificationRequired,
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID: rpID, RPDisplayName: "termocode", RPOrigins: []string{origin},
		AuthenticatorSelection: sel, AttestationPreference: protocol.PreferNoAttestation,
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: LoginTTL, TimeoutUVD: LoginTTL},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: RegisterTTL, TimeoutUVD: RegisterTTL},
		},
	})
	if err != nil {
		return nil, err
	}
	return &Passkeys{wa: wa, rpID: rpID, origin: origin, store: store, now: now, logins: map[string]*loginSess{}}, nil
}

// passkeyUser is a device as WebAuthn sees it: a random user handle (no
// user name exists), its name, and at most one credential.
type passkeyUser struct {
	id    []byte
	name  string
	creds []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte                         { return u.id }
func (u *passkeyUser) WebAuthnName() string                       { return u.name }
func (u *passkeyUser) WebAuthnDisplayName() string                { return u.name }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// creationParams: ES256, EdDSA and RS256, in that order.
var creationParams = []protocol.CredentialParameter{
	{Type: protocol.PublicKeyCredentialType, Algorithm: -7},
	{Type: protocol.PublicKeyCredentialType, Algorithm: -8},
	{Type: protocol.PublicKeyCredentialType, Algorithm: -257},
}

// BeginRegistration starts a passkey for a device named name: the
// options for navigator.credentials.create() and the state to finish it.
func (p *Passkeys) BeginRegistration(name string) (*passkeyReg, json.RawMessage, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, nil, err
	}
	u := &passkeyUser{id: id, name: webdevices.CleanName(name)}
	creation, data, err := p.wa.BeginRegistration(u, webauthn.WithCredentialParameters(creationParams),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation))
	if err != nil {
		return nil, nil, err
	}
	opts, err := json.Marshal(creation)
	if err != nil {
		return nil, nil, err
	}
	return &passkeyReg{user: u, data: *data, until: p.now().Add(RegisterTTL)}, opts, nil
}

// FinishRegistration checks the browser's answer and returns the device
// fields of the new passkey (public key, id, sign count, user handle).
func (p *Passkeys) FinishRegistration(reg *passkeyReg, answer json.RawMessage) (webdevices.Device, error) {
	if reg == nil || !p.now().Before(reg.until) {
		return webdevices.Device{}, ErrLoginExpired
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(answer)
	if err != nil {
		return webdevices.Device{}, ErrPasskey
	}
	cred, err := p.wa.CreateCredential(reg.user, reg.data, parsed)
	if err != nil || !cred.Flags.UserVerified {
		return webdevices.Device{}, ErrPasskey
	}
	return webdevices.Device{CredID: cred.ID, PublicKey: cred.PublicKey, SignCount: cred.Authenticator.SignCount,
		AAGUID: cred.Authenticator.AAGUID, UserID: reg.user.id, RPID: p.rpID}, nil
}

// BeginLogin starts a sign-in with any passkey of this site: the
// options for navigator.credentials.get() and the id to finish it with.
func (p *Passkeys) BeginLogin() (string, json.RawMessage, error) {
	assertion, data, err := p.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return "", nil, err
	}
	opts, err := json.Marshal(assertion)
	if err != nil {
		return "", nil, err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	id := hex.EncodeToString(b)
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, s := range p.logins {
		if !now.Before(s.until) {
			delete(p.logins, k)
		}
	}
	if len(p.logins) >= maxLogins {
		return "", nil, ErrTooManyLogins
	}
	p.logins[id] = &loginSess{data: *data, until: now.Add(LoginTTL)}
	return id, opts, nil
}

// FinishLogin checks an answer to login id (once) and returns the device
// it signs in. A sign count that went back marks the device for
// re-pairing (clone suspected).
func (p *Passkeys) FinishLogin(id string, answer json.RawMessage) (webdevices.Device, error) {
	p.mu.Lock()
	s := p.logins[id]
	delete(p.logins, id) // one use, whatever the answer
	p.mu.Unlock()
	if s == nil || !p.now().Before(s.until) {
		return webdevices.Device{}, ErrLoginExpired
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(answer)
	if err != nil {
		return webdevices.Device{}, ErrPasskey
	}
	var dev webdevices.Device
	var found, repair bool
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		ds, err := p.store.List()
		if err != nil {
			return nil, err
		}
		for _, d := range ds {
			if d.RPID == p.rpID && bytes.Equal(d.CredID, rawID) && bytes.Equal(d.UserID, userHandle) {
				dev, found, repair = d, true, d.NeedsRepair
				return &passkeyUser{id: d.UserID, name: d.Name, creds: []webauthn.Credential{{
					ID: d.CredID, PublicKey: d.PublicKey,
					Authenticator: webauthn.Authenticator{AAGUID: d.AAGUID, SignCount: d.SignCount},
					Flags:         webauthn.CredentialFlags{UserPresent: true, UserVerified: true},
				}}}, nil
			}
		}
		return nil, ErrDeviceRemoved
	}
	_, cred, err := p.wa.ValidatePasskeyLogin(handler, s.data, parsed)
	switch {
	case !found:
		return webdevices.Device{}, ErrDeviceRemoved
	case repair:
		return webdevices.Device{}, ErrDeviceRemoved
	case err != nil || cred == nil || !cred.Flags.UserVerified:
		return webdevices.Device{}, ErrPasskey
	case cred.Authenticator.CloneWarning:
		_, _ = p.store.Change(dev.ID, func(d *webdevices.Device) { d.NeedsRepair = true })
		return webdevices.Device{}, ErrCloneSuspected
	}
	now := p.now().UTC()
	ok, err := p.store.Change(dev.ID, func(d *webdevices.Device) {
		d.SignCount, d.LastSeen = cred.Authenticator.SignCount, now
	})
	if err != nil || !ok {
		return webdevices.Device{}, ErrDeviceRemoved
	}
	dev.SignCount, dev.LastSeen = cred.Authenticator.SignCount, now
	return dev, nil
}

// passkeyWhy is the audit name of a passkey error.
func passkeyWhy(err error) string {
	switch {
	case errors.Is(err, ErrCloneSuspected):
		return "clone_suspected"
	case errors.Is(err, ErrDeviceRemoved):
		return "removed"
	case errors.Is(err, ErrLoginExpired):
		return "expired"
	case errors.Is(err, ErrTooManyLogins):
		return "busy"
	}
	return "passkey"
}
