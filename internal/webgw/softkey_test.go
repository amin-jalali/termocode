//go:build unix

package webgw

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
)

// softKey is a passkey in software, for tests: an ES256 key on one
// "device", made and used the way a browser and an authenticator do.
type softKey struct {
	t      *testing.T
	key    *ecdsa.PrivateKey
	credID []byte
	user   []byte // the user handle the server gave at registration
	count  uint32
	// What the authenticator says about the user and where it runs.
	noUV   bool
	origin string
	rpID   string
}

func newSoftKey(t *testing.T, rpID, origin string) *softKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return &softKey{t: t, key: k, credID: id, rpID: rpID, origin: origin}
}

var b64 = base64.RawURLEncoding

// options reads the challenge (and the user handle) from creation or
// request options as the page gets them.
func optionChallenge(t *testing.T, opts json.RawMessage) (challenge string, user []byte) {
	t.Helper()
	var o struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			User      struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(opts, &o); err != nil || o.PublicKey.Challenge == "" {
		t.Fatalf("options %s: %v", opts, err)
	}
	u, _ := b64.DecodeString(o.PublicKey.User.ID)
	return o.PublicKey.Challenge, u
}

func (s *softKey) flags() byte {
	f := byte(0x01) // UP
	if !s.noUV {
		f |= 0x04 // UV
	}
	return f
}

func (s *softKey) authData(attested []byte) []byte {
	rp := sha256.Sum256([]byte(s.rpID))
	out := append([]byte{}, rp[:]...)
	f := s.flags()
	if attested != nil {
		f |= 0x40 // AT
	}
	out = append(out, f)
	out = binary.BigEndian.AppendUint32(out, s.count)
	return append(out, attested...)
}

func (s *softKey) clientData(typ, challenge string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": s.origin, "crossOrigin": false})
	return b
}

// create answers creation options with a "none" attestation.
func (s *softKey) create(opts json.RawMessage) json.RawMessage {
	s.t.Helper()
	challenge, user := optionChallenge(s.t, opts)
	s.user = user
	pub := s.key.PublicKey
	cose, err := webauthncbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: pad32(pub.X.Bytes()), -3: pad32(pub.Y.Bytes())})
	if err != nil {
		s.t.Fatal(err)
	}
	att := make([]byte, 16) // AAGUID
	att = binary.BigEndian.AppendUint16(att, uint16(len(s.credID)))
	att = append(att, s.credID...)
	att = append(att, cose...)
	obj, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": s.authData(att)})
	if err != nil {
		s.t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{
		"id": b64.EncodeToString(s.credID), "rawId": b64.EncodeToString(s.credID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(s.clientData("webauthn.create", challenge)),
			"attestationObject": b64.EncodeToString(obj),
		},
	})
	return b
}

// get answers request options with an assertion; each use counts up.
func (s *softKey) get(opts json.RawMessage) json.RawMessage {
	s.t.Helper()
	challenge, _ := optionChallenge(s.t, opts)
	s.count++
	ad := s.authData(nil)
	cd := s.clientData("webauthn.get", challenge)
	h := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), h[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, s.key, digest[:])
	if err != nil {
		s.t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{
		"id": b64.EncodeToString(s.credID), "rawId": b64.EncodeToString(s.credID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(cd),
			"authenticatorData": b64.EncodeToString(ad),
			"signature":         b64.EncodeToString(sig),
			"userHandle":        b64.EncodeToString(s.user),
		},
	})
	return b
}

func pad32(b []byte) []byte {
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}
