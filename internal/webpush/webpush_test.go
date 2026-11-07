package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// browser is the phone's side of a subscription: its key pair and auth
// secret, as PushManager.subscribe makes them.
type browser struct {
	priv *ecdh.PrivateKey
	auth []byte
}

func newBrowser(t *testing.T) browser {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return browser{priv: k, auth: auth}
}

func (b browser) sub(endpoint string) Sub {
	return Sub{Endpoint: endpoint, P256dh: b64.EncodeToString(b.priv.PublicKey().Bytes()), Auth: b64.EncodeToString(b.auth)}
}

// decrypt is RFC 8291 from the browser's side: it must read what Encrypt wrote.
func (b browser) decrypt(t *testing.T, body []byte) []byte {
	t.Helper()
	if len(body) < 21+65+16 {
		t.Fatalf("body is %d bytes", len(body))
	}
	salt, rs, idlen := body[:16], binary.BigEndian.Uint32(body[16:20]), int(body[20])
	if rs != 4096 || idlen != 65 {
		t.Fatalf("rs %d idlen %d", rs, idlen)
	}
	asPub, err := ecdh.P256().NewPublicKey(body[21 : 21+idlen])
	if err != nil {
		t.Fatal(err)
	}
	shared, err := b.priv.ECDH(asPub)
	if err != nil {
		t.Fatal(err)
	}
	info := append(append([]byte("WebPush: info\x00"), b.priv.PublicKey().Bytes()...), asPub.Bytes()...)
	ikm, err := hkdf.Key(sha256.New, shared, b.auth, string(info), 32)
	if err != nil {
		t.Fatal(err)
	}
	cek, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	blk, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(blk)
	plain, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	plain = bytes.TrimRight(plain, "\x00")
	if len(plain) == 0 || plain[len(plain)-1] != 2 {
		t.Fatalf("no last-record delimiter: %x", plain)
	}
	return plain[:len(plain)-1]
}

func TestEncryptRoundTrip(t *testing.T) {
	b := newBrowser(t)
	for _, msg := range []string{`{"n":1}`, `{"n":3,"k":"tool"}`, ""} {
		body, err := Encrypt(b.sub("https://fcm.googleapis.com/fcm/send/x"), []byte(msg), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if got := string(b.decrypt(t, body)); got != msg {
			t.Errorf("decrypted %q, want %q", got, msg)
		}
	}
	// Two pushes of the same text never look the same (fresh salt and key).
	x, _ := Encrypt(b.sub("https://fcm.googleapis.com/fcm/send/x"), []byte("same"), rand.Reader)
	y, _ := Encrypt(b.sub("https://fcm.googleapis.com/fcm/send/x"), []byte("same"), rand.Reader)
	if bytes.Equal(x, y) {
		t.Error("two encryptions are equal")
	}
}

func TestEncryptRefusesBadKeys(t *testing.T) {
	b := newBrowser(t)
	good := b.sub("https://fcm.googleapis.com/fcm/send/x")
	for name, s := range map[string]Sub{
		"short auth":  {Endpoint: good.Endpoint, P256dh: good.P256dh, Auth: b64.EncodeToString([]byte("short"))},
		"bad p256dh":  {Endpoint: good.Endpoint, P256dh: b64.EncodeToString(make([]byte, 65)), Auth: good.Auth},
		"not base64":  {Endpoint: good.Endpoint, P256dh: "***", Auth: good.Auth},
		"empty field": {Endpoint: good.Endpoint},
	} {
		if _, err := Encrypt(s, []byte("x"), rand.Reader); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := Encrypt(good, make([]byte, MaxPayload+1), rand.Reader); err == nil {
		t.Error("a too big payload was taken")
	}
}

func TestKeyRoundTripAndPublic(t *testing.T) {
	k, err := NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := ParseKey(k.Private())
	if err != nil {
		t.Fatal(err)
	}
	if k2.Public() != k.Public() {
		t.Error("parsed key has another public key")
	}
	pub, err := b64.DecodeString(k.Public())
	if err != nil || len(pub) != 65 || pub[0] != 4 {
		t.Errorf("public key is not an uncompressed P-256 point: %d bytes, %v", len(pub), err)
	}
	for _, bad := range []string{"", "***", b64.EncodeToString(make([]byte, 31)), b64.EncodeToString(make([]byte, 32))} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) took it", bad)
		}
	}
}

// The VAPID header is a JWT signed with ES256 for the push service's
// origin, valid for at most a day, and names our public key.
func TestAuthHeaderIsAValidVAPIDToken(t *testing.T) {
	k, _ := NewKey(rand.Reader)
	now := time.Unix(1_800_000_000, 0)
	h, err := k.Auth("https://web.push.apple.com/QGxyz/abc?x=1", "mailto:me@example.org", now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "vapid t=") || !strings.HasSuffix(h, ", k="+k.Public()) {
		t.Fatalf("header %q", h)
	}
	jwt := strings.TrimSuffix(strings.TrimPrefix(h, "vapid t="), ", k="+k.Public())
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d parts", len(parts))
	}
	var head struct{ Typ, Alg string }
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	hb, _ := b64.DecodeString(parts[0])
	cb, _ := b64.DecodeString(parts[1])
	if json.Unmarshal(hb, &head) != nil || json.Unmarshal(cb, &claims) != nil {
		t.Fatalf("jwt parts do not parse: %s %s", hb, cb)
	}
	if head.Alg != "ES256" || head.Typ != "JWT" {
		t.Errorf("head %+v", head)
	}
	if claims.Aud != "https://web.push.apple.com" || claims.Sub != "mailto:me@example.org" {
		t.Errorf("claims %+v", claims)
	}
	if left := time.Unix(claims.Exp, 0).Sub(now); left <= 0 || left > 24*time.Hour {
		t.Errorf("exp is %v from now", left)
	}
	sig, _ := b64.DecodeString(parts[2])
	if len(sig) != 64 {
		t.Fatalf("signature is %d bytes", len(sig))
	}
	pubBytes, _ := b64.DecodeString(k.Public())
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), pubBytes)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Error("the signature does not verify")
	}
}

func TestCheckEndpoint(t *testing.T) {
	for _, ok := range []string{
		"https://fcm.googleapis.com/fcm/send/abc:def",
		"https://updates.push.services.mozilla.com/wpush/v2/gAAAA",
		"https://web.push.apple.com/QGx",
		"https://db5p.notify.windows.com/w/?token=AwYAAA",
		"https://fcm.googleapis.com:443/fcm/send/abc",
	} {
		if err := CheckEndpoint(ok, nil); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"http://fcm.googleapis.com/fcm/send/abc",    // not https
		"https://evil.example/fcm/send/abc",         // not a push service
		"https://fcm.googleapis.com.evil.example/x", // a look-alike
		"https://user:pw@fcm.googleapis.com/x",      // userinfo
		"https://fcm.googleapis.com:8443/x",         // another port
		"https://127.0.0.1/x", "https://[::1]/x",    // the laptop itself
		"https://notify.windows.com.evil.example/w",                      // suffix look-alike
		"https://fcm.googleapis.com/" + strings.Repeat("a", MaxEndpoint), // too long
		"", "fcm.googleapis.com/x", "https:///x",
	} {
		if err := CheckEndpoint(bad, nil); err == nil {
			t.Errorf("%s: taken", bad)
		}
	}
	if err := CheckEndpoint("https://127.0.0.1:9999/x", []string{"127.0.0.1:9999"}); err != nil {
		t.Errorf("an allowed test host was refused: %v", err)
	}
}

func TestPayloadHoldsNoContent(t *testing.T) {
	if p := Payload(DetailNone, 3, "tool"); p != nil {
		t.Errorf("none: %q", p)
	}
	if p := string(Payload(DetailCount, 3, "tool")); p != `{"n":3}` {
		t.Errorf("count: %q", p)
	}
	if p := string(Payload(DetailTitle, 2, "tool")); p != `{"n":2,"k":"tool"}` {
		t.Errorf("title: %q", p)
	}
	// Only a kind of the closed list goes out; anything else is dropped.
	if p := string(Payload(DetailTitle, 1, "rm -rf /secret")); p != `{"n":1}` {
		t.Errorf("title with a free text: %q", p)
	}
	if p := string(Payload("weird", 1, "tool")); p != `{"n":1}` {
		t.Errorf("unknown detail is not count: %q", p)
	}
}

func TestSendPostsAnEncryptedPush(t *testing.T) {
	b := newBrowser(t)
	k, _ := NewKey(rand.Reader)
	var got *http.Request
	var body []byte
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	s := &Sender{Key: k, Subject: "mailto:me@example.org", Client: srv.Client(), Hosts: []string{u.Host}}
	gone, err := s.Send(context.Background(), b.sub(srv.URL+"/push/abc"), []byte(`{"n":1}`))
	if err != nil || gone {
		t.Fatalf("send: gone %v err %v", gone, err)
	}
	if got.Method != http.MethodPost || got.URL.Path != "/push/abc" {
		t.Errorf("request %s %s", got.Method, got.URL.Path)
	}
	for h, want := range map[string]string{"Content-Encoding": "aes128gcm", "Content-Type": "application/octet-stream", "Urgency": "high"} {
		if got.Header.Get(h) != want {
			t.Errorf("%s = %q, want %q", h, got.Header.Get(h), want)
		}
	}
	if got.Header.Get("TTL") == "" || !strings.HasPrefix(got.Header.Get("Authorization"), "vapid t=") {
		t.Errorf("TTL %q Authorization %q", got.Header.Get("TTL"), got.Header.Get("Authorization"))
	}
	if string(b.decrypt(t, body)) != `{"n":1}` {
		t.Error("the phone cannot read the push")
	}
}

func TestSendNoPayloadHasNoBody(t *testing.T) {
	b := newBrowser(t)
	k, _ := NewKey(rand.Reader)
	var n int
	var enc string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bb, _ := io.ReadAll(r.Body)
		n, enc = len(bb), r.Header.Get("Content-Encoding")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	s := &Sender{Key: k, Subject: "mailto:me@example.org", Client: srv.Client(), Hosts: []string{u.Host}}
	if _, err := s.Send(context.Background(), b.sub(srv.URL+"/p"), nil); err != nil {
		t.Fatal(err)
	}
	if n != 0 || enc != "" {
		t.Errorf("a wake-only push has %d bytes, encoding %q", n, enc)
	}
}

func TestSendGoneAndErrors(t *testing.T) {
	b := newBrowser(t)
	k, _ := NewKey(rand.Reader)
	code := http.StatusGone
	redirected := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			redirected = true
		}
		if code == http.StatusFound {
			http.Redirect(w, r, "/elsewhere", code)
			return
		}
		w.WriteHeader(code)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	s := &Sender{Key: k, Subject: "mailto:me@example.org", Client: srv.Client(), Hosts: []string{u.Host}}
	for _, c := range []int{http.StatusGone, http.StatusNotFound} {
		code = c
		gone, err := s.Send(context.Background(), b.sub(srv.URL+"/p"), []byte("x"))
		if !gone || err != nil {
			t.Errorf("%d: gone %v err %v", c, gone, err)
		}
	}
	for _, c := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusFound} {
		code = c
		gone, err := s.Send(context.Background(), b.sub(srv.URL+"/p"), []byte("x"))
		if gone || err == nil {
			t.Errorf("%d: gone %v err %v", c, gone, err)
		}
		if err != nil && strings.Contains(err.Error(), "/p") {
			t.Errorf("%d: the error names the endpoint: %v", c, err)
		}
	}
	if redirected {
		t.Error("a redirect was followed")
	}
	if _, err := s.Send(context.Background(), b.sub("https://evil.example/p"), []byte("x")); err == nil {
		t.Error("a push went to a host that is not a push service")
	}
}
