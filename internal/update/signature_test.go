package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/blake2b"
)

var testKeyID = []byte{1, 2, 3, 4, 5, 6, 7, 8}

func testKey(t *testing.T) (string, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw := append(append([]byte("Ed"), testKeyID...), pub...)
	return base64.StdEncoding.EncodeToString(raw), priv
}

func testSig(priv ed25519.PrivateKey, alg string, keyID, msg []byte) []byte {
	signed := msg
	if alg == "ED" {
		h := blake2b.Sum512(msg)
		signed = h[:]
	}
	raw := append(append([]byte(alg), keyID...), ed25519.Sign(priv, signed)...)
	return []byte("untrusted comment: test\n" + base64.StdEncoding.EncodeToString(raw) + "\ntrusted comment: t\nAAAA\n")
}

func TestVerifyMinisign(t *testing.T) {
	pub, priv := testKey(t)
	msg := []byte("abc  termocode.tar.gz\n")
	for _, alg := range []string{"Ed", "ED"} {
		if err := VerifyMinisign(pub, msg, testSig(priv, alg, testKeyID, msg)); err != nil {
			t.Errorf("%s: %v", alg, err)
		}
		if err := VerifyMinisign(pub, []byte("changed"), testSig(priv, alg, testKeyID, msg)); err == nil {
			t.Errorf("%s: a changed file passed", alg)
		}
	}
	if err := VerifyMinisign(pub, msg, testSig(priv, "ED", []byte{9, 9, 9, 9, 9, 9, 9, 9}, msg)); err == nil || !strings.Contains(err.Error(), "another key") {
		t.Errorf("other key: %v", err)
	}
	for _, bad := range []string{"", "x", "untrusted comment: x\n!!!\n"} {
		if err := VerifyMinisign(pub, msg, []byte(bad)); err == nil {
			t.Errorf("garbage %q passed", bad)
		}
	}
	if err := VerifyMinisign("nonsense", msg, testSig(priv, "ED", testKeyID, msg)); err == nil {
		t.Error("a bad public key passed")
	}
}

func TestPrepareSignatureRules(t *testing.T) {
	pub, priv := testKey(t)
	sums := []byte("abc  file\n")
	var served []byte
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(served) }))
	defer srv.Close()
	c := Client{HTTP: srv.Client()}
	rel := Release{Tag: "v1.0.0", Assets: []Asset{{Name: SignatureName, URL: srv.URL + "/sig"}}}
	old := releasePublicKey
	t.Cleanup(func() { releasePublicKey = old })

	releasePublicKey = "" // placeholder: off, updates keep working
	if err := c.verifySignature(context.Background(), Release{Tag: "v1.0.0"}, sums); err != nil {
		t.Fatalf("with no key set: %v", err)
	}
	releasePublicKey = pub
	served = testSig(priv, "ED", testKeyID, sums)
	if err := c.verifySignature(context.Background(), rel, sums); err != nil {
		t.Fatalf("good signature: %v", err)
	}
	if err := c.verifySignature(context.Background(), rel, []byte("tampered")); err == nil {
		t.Fatal("tampered checksums passed")
	}
	if err := c.verifySignature(context.Background(), Release{Tag: "v1.0.0"}, sums); err == nil || !strings.Contains(err.Error(), "no signature") {
		t.Fatalf("missing signature: %v", err)
	}
}
