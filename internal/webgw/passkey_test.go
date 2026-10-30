//go:build unix

package webgw

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webdevices"
)

const pkHost = "box.tail1234.ts.net"

func newTestPasskeys(t *testing.T) (*Passkeys, *webdevices.Store) {
	t.Helper()
	site, _ := TailnetSite(pkHost)
	st := webdevices.Open(filepath.Join(t.TempDir(), "web"))
	p, err := NewPasskeys(site, st, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return p, st
}

// register makes a passkey for a new device and stores the device.
func register(t *testing.T, p *Passkeys, st *webdevices.Store, k *softKey) webdevices.Device {
	t.Helper()
	reg, opts, err := p.BeginRegistration("Phone")
	if err != nil {
		t.Fatal(err)
	}
	d, err := p.FinishRegistration(reg, k.create(opts))
	if err != nil {
		t.Fatalf("finish registration: %v", err)
	}
	d.ID, d.Name, d.Role = "dev-00000001", "Phone", webdevices.RoleViewer
	if err := st.Add(d); err != nil {
		t.Fatal(err)
	}
	return d
}

func login(t *testing.T, p *Passkeys, k *softKey) (webdevices.Device, error) {
	t.Helper()
	id, opts, err := p.BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	return p.FinishLogin(id, k.get(opts))
}

// Spec (WEB-33): a passkey made in pairing signs the device in later;
// the options ask for a discoverable key with user verification and no
// attestation, and the store keeps only the public key.
func TestPasskeyRegisterAndLogin(t *testing.T) {
	p, st := newTestPasskeys(t)
	_, opts, _ := p.BeginRegistration("Phone")
	var o struct {
		PublicKey struct {
			RP                     struct{ ID string } `json:"rp"`
			AuthenticatorSelection struct {
				ResidentKey      string `json:"residentKey"`
				UserVerification string `json:"userVerification"`
			} `json:"authenticatorSelection"`
			Attestation string `json:"attestation"`
		} `json:"publicKey"`
	}
	_ = json.Unmarshal(opts, &o)
	if o.PublicKey.RP.ID != pkHost || o.PublicKey.AuthenticatorSelection.ResidentKey != "required" ||
		o.PublicKey.AuthenticatorSelection.UserVerification != "required" || o.PublicKey.Attestation != "none" {
		t.Fatalf("creation options %s", opts)
	}
	k := newSoftKey(t, pkHost, "https://"+pkHost)
	d := register(t, p, st, k)
	if len(d.PublicKey) == 0 || string(d.CredID) != string(k.credID) || d.RPID != pkHost {
		t.Fatalf("device %+v", d)
	}
	got, err := login(t, p, k)
	if err != nil || got.ID != d.ID {
		t.Fatalf("login: %+v %v", got, err)
	}
	if after, _, _ := st.Get(d.ID); after.SignCount != 1 || after.LastSeen.IsZero() {
		t.Fatalf("sign count / last seen not kept: %+v", after)
	}
}

// Spec (WEB-33, T30): without user verification (no Face ID / PIN) the
// passkey is refused, at registration and at login.
func TestUVRequired(t *testing.T) {
	p, st := newTestPasskeys(t)
	k := newSoftKey(t, pkHost, "https://"+pkHost)
	k.noUV = true
	reg, opts, _ := p.BeginRegistration("Phone")
	if _, err := p.FinishRegistration(reg, k.create(opts)); err == nil {
		t.Fatal("a passkey without UV was registered")
	}
	k.noUV = false
	register(t, p, st, k)
	k.noUV = true
	if _, err := login(t, p, k); err == nil {
		t.Fatal("a login without UV passed")
	}
}

// Spec (WEB-33, phishing): an assertion made on another origin, or for
// another RP, is refused even with the right key.
func TestLoginWrongOriginRefused(t *testing.T) {
	p, st := newTestPasskeys(t)
	k := newSoftKey(t, pkHost, "https://"+pkHost)
	register(t, p, st, k)
	k.origin = "https://evil.tail1234.ts.net"
	if _, err := login(t, p, k); err == nil {
		t.Fatal("other origin accepted")
	}
	k.origin, k.rpID = "https://"+pkHost, "evil.tail1234.ts.net"
	if _, err := login(t, p, k); err == nil {
		t.Fatal("other RP accepted")
	}
}

// Spec (WEB-33, replay): a login challenge works once; the same signed
// answer sent again is refused.
func TestChallengeSingleUse(t *testing.T) {
	p, st := newTestPasskeys(t)
	k := newSoftKey(t, pkHost, "https://"+pkHost)
	register(t, p, st, k)
	id, opts, _ := p.BeginLogin()
	ans := k.get(opts)
	if _, err := p.FinishLogin(id, ans); err != nil {
		t.Fatal(err)
	}
	if _, err := p.FinishLogin(id, ans); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("replay: %v", err)
	}
}

// Spec (WEB-33, T30): a sign count that goes back means a cloned key:
// refused, the device must pair again, and the error says so.
func TestSignCountRegressionRefused(t *testing.T) {
	p, st := newTestPasskeys(t)
	k := newSoftKey(t, pkHost, "https://"+pkHost)
	d := register(t, p, st, k)
	if _, err := login(t, p, k); err != nil {
		t.Fatal(err)
	}
	if _, err := login(t, p, k); err != nil {
		t.Fatal(err)
	}
	k.count = 0 // the clone starts behind
	if _, err := login(t, p, k); !errors.Is(err, ErrCloneSuspected) {
		t.Fatalf("err = %v", err)
	}
	if after, _, _ := st.Get(d.ID); !after.NeedsRepair {
		t.Fatal("device not marked for re-pairing")
	}
	k.count = 10
	if _, err := login(t, p, k); !errors.Is(err, ErrDeviceRemoved) {
		t.Fatalf("a device that must pair again signed in: %v", err)
	}
}

// Spec (WEB-33, T30): a removed device's passkey signs in nowhere.
func TestRevokedDeviceCannotLogin(t *testing.T) {
	p, st := newTestPasskeys(t)
	k := newSoftKey(t, pkHost, "https://"+pkHost)
	d := register(t, p, st, k)
	if _, _, err := st.Remove(d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := login(t, p, k); !errors.Is(err, ErrDeviceRemoved) {
		t.Fatalf("err = %v", err)
	}
}

// A login session left unused expires; and there is a cap on open ones.
func TestLoginSessionExpiresAndCaps(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	site, _ := TailnetSite(pkHost)
	st := webdevices.Open(filepath.Join(t.TempDir(), "web"))
	p, _ := NewPasskeys(site, st, clock.Now)
	k := newSoftKey(t, pkHost, "https://"+pkHost)
	register(t, p, st, k)
	id, opts, _ := p.BeginLogin()
	clock.Add(LoginTTL)
	if _, err := p.FinishLogin(id, k.get(opts)); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("late login: %v", err)
	}
	for i := 0; i < maxLogins; i++ {
		if _, _, err := p.BeginLogin(); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := p.BeginLogin(); err == nil {
		t.Fatal("no cap on open logins")
	}
}

// A loopback site is a passkey site only as localhost (the RP id may not
// be an IP address): this is what the browser tests use.
func TestPasskeysOnLocalhost(t *testing.T) {
	p, err := NewPasskeys(LoopbackSite(41237), webdevices.Open(t.TempDir()), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if p.rpID != "localhost" || p.origin != "http://localhost:41237" {
		t.Fatalf("rp %q origin %q", p.rpID, p.origin)
	}
}
