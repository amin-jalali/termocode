package webpush

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
)

// MaxPayload is the most plaintext one push carries (RFC 8291: 4096
// bytes of record, less the header, the tag and the delimiter).
const MaxPayload = 3993

// recordSize is the one record's size field (RFC 8188).
const recordSize = 4096

// Sub is one browser's push subscription, as PushSubscription.toJSON()
// gives it: the push service's endpoint and the browser's keys.
type Sub struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"` // the browser's P-256 public key, base64url
	Auth     string `json:"auth"`   // the 16-byte auth secret, base64url
}

// errBadSub never names the values.
var errBadSub = errors.New("webpush: bad subscription keys")

// keys decodes and checks the subscription's keys.
func (s Sub) keys() (*ecdh.PublicKey, []byte, error) {
	pb, err := b64.DecodeString(s.P256dh)
	if err != nil || len(pb) != 65 {
		return nil, nil, errBadSub
	}
	ua, err := ecdh.P256().NewPublicKey(pb)
	if err != nil {
		return nil, nil, errBadSub
	}
	auth, err := b64.DecodeString(s.Auth)
	if err != nil || len(auth) != 16 {
		return nil, nil, errBadSub
	}
	return ua, auth, nil
}

// Check says whether the subscription's keys are well formed.
func (s Sub) Check() error {
	_, _, err := s.keys()
	return err
}

// Encrypt is RFC 8291: plain encrypted for the browser of s as one
// aes128gcm record, with a fresh key pair and salt from r.
func Encrypt(s Sub, plain []byte, r io.Reader) ([]byte, error) {
	if len(plain) > MaxPayload {
		return nil, errors.New("webpush: payload too big")
	}
	ua, auth, err := s.keys()
	if err != nil {
		return nil, err
	}
	as, err := ecdh.P256().GenerateKey(r)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(r, salt); err != nil {
		return nil, err
	}
	shared, err := as.ECDH(ua)
	if err != nil {
		return nil, errBadSub
	}
	asPub := as.PublicKey().Bytes()
	info := append(append([]byte("WebPush: info\x00"), ua.Bytes()...), asPub...)
	ikm, err := hkdf.Key(sha256.New, shared, auth, string(info), 32)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	blk, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	head := make([]byte, 0, 21+len(asPub))
	head = append(head, salt...)
	head = binary.BigEndian.AppendUint32(head, recordSize)
	head = append(head, byte(len(asPub)))
	head = append(head, asPub...)
	rec := append(append(make([]byte, 0, len(plain)+1), plain...), 2) // 2: the last record
	return gcm.Seal(head, nonce, rec, nil), nil
}
