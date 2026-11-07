package webpush

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"time"
)

var b64 = base64.RawURLEncoding

// TokenTTL is how long a VAPID token is valid (the services allow at
// most 24 hours).
const TokenTTL = 12 * time.Hour

// ErrBadKey: a stored VAPID key does not parse.
var ErrBadKey = errors.New("webpush: bad VAPID key")

// Key is the laptop's VAPID key pair (P-256).
type Key struct {
	priv *ecdsa.PrivateKey
}

// NewKey makes a fresh key pair from r (crypto/rand.Reader).
func NewKey(r io.Reader) (*Key, error) {
	p, err := ecdsa.GenerateKey(elliptic.P256(), r)
	if err != nil {
		return nil, err
	}
	return &Key{priv: p}, nil
}

// ParseKey reads a key that Private wrote.
func ParseKey(s string) (*Key, error) {
	d, err := b64.DecodeString(s)
	if err != nil || len(d) != 32 {
		return nil, ErrBadKey
	}
	p, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), d)
	if err != nil {
		return nil, ErrBadKey
	}
	return &Key{priv: p}, nil
}

// Private is the private scalar, base64url: for the secret store only.
func (k *Key) Private() string {
	d, err := k.priv.Bytes()
	if err != nil {
		return ""
	}
	return b64.EncodeToString(d)
}

// Public is the public key as the page's applicationServerKey wants it:
// the uncompressed point, base64url.
func (k *Key) Public() string {
	b, err := k.priv.PublicKey.Bytes()
	if err != nil {
		return ""
	}
	return b64.EncodeToString(b)
}

// Auth is the Authorization header of a push to endpoint: a JWT for the
// endpoint's origin, signed with ES256, and the public key.
func (k *Key) Auth(endpoint, subject string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("webpush: bad endpoint")
	}
	host := u.Host
	if u.Port() == "443" {
		host = u.Hostname()
	}
	head := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}{u.Scheme + "://" + host, now.Add(TokenTTL).Unix(), subject})
	if err != nil {
		return "", err
	}
	signed := head + "." + b64.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signed))
	r, s, err := ecdsa.Sign(rand.Reader, k.priv, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return "vapid t=" + signed + "." + b64.EncodeToString(sig) + ", k=" + k.Public(), nil
}
