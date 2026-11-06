//go:build unix

package webgw

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"
)

// Spec (WEBP-12): a bookmark key is "tcb1.<device id>.<43 base64url
// chars>" (32 random bytes). The laptop keeps only its SHA-256; two keys
// of one device differ; the parser takes back exactly what was made.
func TestDeviceKeyRoundTrip(t *testing.T) {
	k1, h1, err := NewDeviceKey("dev-1a2b3c4d")
	if err != nil {
		t.Fatal(err)
	}
	k2, h2, _ := NewDeviceKey("dev-1a2b3c4d")
	if k1 == k2 || bytes.Equal(h1, h2) {
		t.Fatal("two keys are the same")
	}
	if !strings.HasPrefix(k1, "tcb1.dev-1a2b3c4d.") || len(k1) != DeviceKeyLen {
		t.Fatalf("key shape %q", k1)
	}
	dev, sec, err := ParseDeviceKey(k1)
	if err != nil || dev != "dev-1a2b3c4d" {
		t.Fatalf("parse: %q %v", dev, err)
	}
	if sum := sha256.Sum256(sec); !bytes.Equal(sum[:], h1) || !bytes.Equal(DeviceKeyHash(sec), h1) {
		t.Fatal("the hash is not the SHA-256 of the secret")
	}
	if bytes.Contains(h1, sec) {
		t.Fatal("the hash holds the secret")
	}
	if _, _, err := NewDeviceKey("not-a-device"); err == nil {
		t.Fatal("a key for a bad device id")
	}
}

// Every key that is not exactly in shape is refused, and the error never
// quotes the key.
func TestParseDeviceKeyRefuses(t *testing.T) {
	good, _, _ := NewDeviceKey("dev-00ff00ff")
	sec := good[len("tcb1.dev-00ff00ff."):]
	for _, bad := range []string{
		"", "tcb1", "tcb1..", good + "A", good[:len(good)-1],
		"tcb2.dev-00ff00ff." + sec, "TCB1.dev-00ff00ff." + sec,
		"tcb1.dev-00FF00FF." + sec, "tcb1.dev-00ff00f." + sec + "A", "tcb1.xyz-00ff00ff." + sec,
		"tcb1.dev-00ff00ff_" + sec, "tcb1.dev-00ff00ff." + strings.Repeat("=", 43),
		"tcb1.dev-00ff00ff." + sec[:42] + "+", "tcb1.dev-00ff00ff." + sec[:42] + "/",
		"tcb1.dev-00ff00ff." + sec[:42] + ".", " " + good[1:], good[:len(good)-1] + "\n",
		"tcb1.dev-00ff00ff." + sec[:42] + "B", // non-canonical last char (spare bits set)
	} {
		if bad == good {
			continue
		}
		dev, s, err := ParseDeviceKey(bad)
		if err == nil {
			t.Errorf("parsed %q as %q", bad, dev)
		}
		if s != nil || dev != "" {
			t.Errorf("%q: gave parts back with an error", bad)
		}
		if err != nil && len(bad) > 8 && strings.Contains(err.Error(), bad) {
			t.Errorf("error quotes the key: %v", err)
		}
	}
}

// FuzzParseDeviceKey: no panic; anything that parses is in canonical shape
// (formatting it again gives the same text), so one secret has exactly
// one spelling and a key can never be smuggled in another form.
func FuzzParseDeviceKey(f *testing.F) {
	good, _, _ := NewDeviceKey("dev-1a2b3c4d")
	for _, s := range []string{good, "", "tcb1.", "tcb1.dev-1a2b3c4d.", good + "=", strings.ToUpper(good), good[:60] + "\x00",
		"tcb1.dev-1a2b3c4d." + strings.Repeat("_", 43), "tcb1.dev-1a2b3c4d." + strings.Repeat("-", 43)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		dev, sec, err := ParseDeviceKey(s)
		if err != nil {
			if dev != "" || sec != nil {
				t.Fatal("parts with an error")
			}
			return
		}
		if len(sec) != 32 || !isDeviceID(dev) {
			t.Fatalf("parsed parts %q %d", dev, len(sec))
		}
		if formatDeviceKey(dev, sec) != s {
			t.Fatalf("not canonical: %q", s)
		}
	})
}
