// Package relaytun is the end-to-end layer of the web relay (ADR 0022,
// WEB-53): a Noise IK channel between the phone's page (the initiator,
// which knows the gateway's static key from the QR code) and the
// gateway (the responder), carried as one message per relay WebSocket
// message, with many byte streams inside it (one per HTTP request or
// WebSocket of the page).
//
// The suite is Noise_IK_25519_AESGCM_SHA256: X25519, AES-256-GCM and
// SHA-256 are all in the browsers' own WebCrypto, so the page carries
// no crypto library of its own. The relay sees the handshake's public
// ephemeral keys and then only ciphertext; it cannot read, change,
// drop or reorder a message without the session ending.
package relaytun

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/coder/websocket"
	"github.com/flynn/noise"
	"golang.org/x/crypto/curve25519"
)

// Prologue binds both ends to this protocol and version.
var Prologue = []byte("termocode-relay-v1")

// Suite is the Noise cipher suite: every primitive is in WebCrypto.
var Suite = noise.NewCipherSuite(noise.DH25519, noise.CipherAESGCM, noise.HashSHA256)

// MaxMsg is the largest Noise message (the Noise limit).
const MaxMsg = 65535

// PeerKey is an X25519 public key.
type PeerKey [32]byte

// String is the key as 43 base64url characters (no padding), as the QR
// code and the page carry it.
func (k PeerKey) String() string { return base64.RawURLEncoding.EncodeToString(k[:]) }

// ParsePeerKey reads a key written by String.
func ParsePeerKey(s string) (PeerKey, error) {
	var k PeerKey
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) != len(k) {
		return k, errors.New("relay key: want 43 base64url characters")
	}
	copy(k[:], b)
	return k, nil
}

// Keypair is a static X25519 key pair.
type Keypair struct {
	Private [32]byte
	Public  PeerKey
}

// GenerateKeypair makes a new static key pair.
func GenerateKeypair() (Keypair, error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return Keypair{}, err
	}
	return KeypairFromPrivate(priv)
}

// KeypairFromPrivate rebuilds a key pair from its stored private key.
func KeypairFromPrivate(priv [32]byte) (Keypair, error) {
	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return Keypair{}, err
	}
	kp := Keypair{Private: priv}
	copy(kp.Public[:], pub)
	return kp, nil
}

func (k Keypair) dh() noise.DHKey {
	return noise.DHKey{Private: append([]byte(nil), k.Private[:]...), Public: append([]byte(nil), k.Public[:]...)}
}

// Hello is the page's first, encrypted payload.
type Hello struct {
	V int `json:"v"`
	// Pair is the pairing secret of the QR code a new phone scanned; a
	// phone the gateway knows sends none.
	Pair string `json:"pair,omitempty"`
}

// welcome is the gateway's answer, the second handshake payload.
type welcome struct {
	OK  bool   `json:"ok"`
	Why string `json:"why,omitempty"`
}

// Allow decides, after the first handshake message, whether a page may
// go on: peer is its proven static key.
type Allow func(peer PeerKey, hello Hello) error

// ErrNotPaired: the page's key is not known and it brought no valid
// pairing secret.
var ErrNotPaired = errors.New("relay: this phone is not paired")

// ErrDecrypt: a message did not decrypt (changed, dropped, reordered or
// from someone else). The session ends.
var ErrDecrypt = errors.New("relay: a message failed to decrypt")

// RefusedError is the gateway's refusal as the page sees it.
type RefusedError struct{ Why string }

func (e *RefusedError) Error() string { return "relay: the laptop refused: " + e.Why }

// MsgConn carries whole messages: the relay's WebSocket, or a test pipe.
type MsgConn interface {
	ReadMsg(ctx context.Context) ([]byte, error)
	WriteMsg(ctx context.Context, b []byte) error
	Close() error
}

// WSConn adapts a relay WebSocket. Only binary messages count; a text
// message is an error.
func WSConn(c *websocket.Conn) MsgConn {
	c.SetReadLimit(MaxMsg + 64)
	return wsConn{c}
}

type wsConn struct{ c *websocket.Conn }

func (w wsConn) ReadMsg(ctx context.Context) ([]byte, error) {
	typ, b, err := w.c.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageBinary {
		return nil, errors.New("relay: a text message inside the tunnel")
	}
	return b, nil
}

func (w wsConn) WriteMsg(ctx context.Context, b []byte) error {
	return w.c.Write(ctx, websocket.MessageBinary, b)
}

func (w wsConn) Close() error { return w.c.Close(websocket.StatusNormalClosure, "") }

func whyOf(err error) string {
	if errors.Is(err, ErrNotPaired) {
		return "not_paired"
	}
	return "refused"
}

// Accept runs the gateway's (responder's) side of the handshake on c.
// allow sees the page's key and hello; its error refuses the page, which
// is told why. On an error c is closed.
func Accept(ctx context.Context, c MsgConn, key Keypair, allow Allow) (*Session, error) {
	hs, err := noise.NewHandshakeState(noise.Config{CipherSuite: Suite, Pattern: noise.HandshakeIK,
		Prologue: Prologue, StaticKeypair: key.dh()})
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	m1, err := c.ReadMsg(ctx)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	if len(m1) > MaxMsg {
		_ = c.Close()
		return nil, errors.New("relay: handshake message too big")
	}
	payload, _, _, err := hs.ReadMessage(nil, m1)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("%w (handshake)", ErrDecrypt)
	}
	var peer PeerKey
	copy(peer[:], hs.PeerStatic())
	var hello Hello
	refuse := json.Unmarshal(payload, &hello)
	if refuse == nil && hello.V != 1 {
		refuse = errors.New("relay: unknown version")
	}
	if refuse == nil {
		refuse = allow(peer, hello)
	}
	w := welcome{OK: refuse == nil}
	if refuse != nil {
		w.Why = whyOf(refuse)
	}
	wb, _ := json.Marshal(w)
	m2, recv, send, err := hs.WriteMessage(nil, wb)
	if err == nil {
		err = c.WriteMsg(ctx, m2)
	}
	if refuse != nil || err != nil {
		_ = c.Close()
		if refuse != nil {
			return nil, refuse
		}
		return nil, err
	}
	// The responder's Split gives (initiator→responder, responder→initiator).
	return newSession(c, send, recv, peer, false), nil
}

// Dial runs the page's (initiator's) side: gw is the gateway key from
// the QR code. A gateway that refuses gives a *RefusedError. Headless
// pages (tests, a future app) use it; the browser has its own copy in
// TypeScript.
func Dial(ctx context.Context, c MsgConn, key Keypair, gw PeerKey, hello Hello) (*Session, error) {
	hello.V = 1
	hs, err := noise.NewHandshakeState(noise.Config{CipherSuite: Suite, Pattern: noise.HandshakeIK,
		Initiator: true, Prologue: Prologue, StaticKeypair: key.dh(), PeerStatic: append([]byte(nil), gw[:]...)})
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	hb, _ := json.Marshal(hello)
	m1, _, _, err := hs.WriteMessage(nil, hb)
	if err == nil {
		err = c.WriteMsg(ctx, m1)
	}
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	m2, err := c.ReadMsg(ctx)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	payload, send, recv, err := hs.ReadMessage(nil, m2)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("%w (handshake)", ErrDecrypt)
	}
	var w welcome
	if json.Unmarshal(payload, &w) != nil || !w.OK {
		_ = c.Close()
		why := w.Why
		if why == "" {
			why = "refused"
		}
		return nil, &RefusedError{Why: why}
	}
	return newSession(c, send, recv, gw, true), nil
}
