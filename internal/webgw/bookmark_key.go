//go:build unix

package webgw

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// Bookmark keys (WEBP-12): a paired device that chose "Remember this
// browser" keeps one key in its browser, for its one origin, so it can
// open the panel from a bookmark without the passkey prompt. The key is
//
//	tcb1.<device id>.<32 random bytes, base64url, no padding>
//
// The laptop keeps only the SHA-256 of the random part (devices.json); a
// copy of the file cannot sign in. Every use gives a new key.

// deviceKeyPrefix names the format; a new format gets a new prefix.
const deviceKeyPrefix = "tcb1."

// deviceKeySecretLen is the random part, in bytes.
const deviceKeySecretLen = 32

// DeviceKeyLen is the exact length of a key: prefix, "dev-" + 8 hex,
// a dot, 43 base64url chars.
const DeviceKeyLen = len(deviceKeyPrefix) + 12 + 1 + 43

// ErrBadDeviceKey: the text is not a bookmark key. It never quotes it.
var ErrBadDeviceKey = errors.New("not a bookmark key")

var keyB64 = base64.RawURLEncoding.Strict()

// NewDeviceKey makes a key for a device and the hash the laptop keeps.
func NewDeviceKey(deviceID string) (key string, hash []byte, err error) {
	if !isDeviceID(deviceID) {
		return "", nil, ErrBadDeviceKey
	}
	sec := make([]byte, deviceKeySecretLen)
	if _, err := rand.Read(sec); err != nil {
		return "", nil, err
	}
	return formatDeviceKey(deviceID, sec), DeviceKeyHash(sec), nil
}

// DeviceKeyHash is what the laptop keeps of a key's random part.
func DeviceKeyHash(secret []byte) []byte {
	sum := sha256.Sum256(secret)
	return sum[:]
}

func formatDeviceKey(deviceID string, secret []byte) string {
	return deviceKeyPrefix + deviceID + "." + keyB64.EncodeToString(secret)
}

// ParseDeviceKey splits a key into its device id and random part. Only
// the one canonical spelling passes: exact length, prefix, a device id
// of "dev-" and 8 lower-case hex digits, and strict base64url.
func ParseDeviceKey(s string) (deviceID string, secret []byte, err error) {
	if len(s) != DeviceKeyLen || !strings.HasPrefix(s, deviceKeyPrefix) {
		return "", nil, ErrBadDeviceKey
	}
	rest := s[len(deviceKeyPrefix):]
	dev, enc, ok := strings.Cut(rest, ".")
	if !ok || !isDeviceID(dev) || len(enc) != 43 {
		return "", nil, ErrBadDeviceKey
	}
	sec, err := keyB64.DecodeString(enc)
	if err != nil || len(sec) != deviceKeySecretLen || keyB64.EncodeToString(sec) != enc {
		return "", nil, ErrBadDeviceKey
	}
	return dev, sec, nil
}

// isDeviceID: "dev-" and 8 lower-case hex digits (newDeviceID's shape).
func isDeviceID(s string) bool {
	if len(s) != 12 || !strings.HasPrefix(s, "dev-") {
		return false
	}
	for i := 4; i < 12; i++ {
		c := s[i]
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}
