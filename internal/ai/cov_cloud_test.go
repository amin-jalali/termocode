package ai

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBedrockStreamReadsExceptionsAndSkipsOtherEvents(t *testing.T) {
	var in bytes.Buffer
	in.Write(eventFrame(map[string]string{":message-type": "event", ":event-type": "initial-response"}, []byte("{}")))
	in.Write(chunkFrame("{\"type\":\"a\"}\n"))
	// an exception with a message, one with only a kind, one by error code
	in.Write(eventFrame(map[string]string{":message-type": "exception", ":exception-type": "throttling"}, []byte(`{"message":"slow down"}`)))
	in.Write(eventFrame(map[string]string{":message-type": "exception", ":exception-type": "validation"}, []byte(`{}`)))
	in.Write(eventFrame(map[string]string{":message-type": "error", ":error-code": "E1"}, []byte(`not json`)))
	var out bytes.Buffer
	if err := bedrockToSSE(&in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n\n")
	if len(lines) != 4 {
		t.Fatalf("events %d: %q", len(lines), out.String())
	}
	want := []string{`"type":"a"`, `"message":"slow down"`, `"message":"validation"`, `"message":"E1"`}
	for i, w := range want {
		if !strings.Contains(lines[i], w) {
			t.Errorf("event %d = %s, want %s", i, lines[i], w)
		}
	}
}

func TestBedrockStreamBadChunks(t *testing.T) {
	bad := map[string][]byte{
		"payload not json": eventFrame(map[string]string{":message-type": "event", ":event-type": "chunk"}, []byte("zzz")),
		"bytes not base64": eventFrame(map[string]string{":message-type": "event", ":event-type": "chunk"}, []byte(`{"bytes":"!!"}`)),
	}
	for name, frame := range bad {
		if err := bedrockToSSE(bytes.NewReader(frame), io.Discard); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	// a writer that fails stops the stream
	if err := bedrockToSSE(bytes.NewReader(chunkFrame("{}")), failWriter{}); err == nil {
		t.Error("a failing writer must stop the stream")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestEventFrameDamageIsDetected(t *testing.T) {
	good := chunkFrame("{}")
	cut := func(n int) []byte { return good[:n] }
	flip := func(i int) []byte { b := append([]byte(nil), good...); b[i] ^= 0xff; return b }
	short := func(total uint32) []byte {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.BigEndian, total)
		_ = binary.Write(&b, binary.BigEndian, uint32(0))
		_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(b.Bytes()))
		b.Write(make([]byte, 8))
		return b.Bytes()
	}
	cases := map[string]struct {
		data []byte
		want string
	}{
		"cut in the first word": {cut(2), "ended inside"},
		"cut in the prelude":    {cut(8), "ended inside"},
		"cut in the body":       {cut(len(good) - 3), "ended inside"},
		"prelude crc":           {flip(9), "prelude CRC"},
		"message crc":           {flip(len(good) - 1), "(CRC)"},
		"length too small":      {short(8), "bad length"},
		"length too big":        {short(bedrockMaxFrame + 1), "bad length"},
	}
	for name, c := range cases {
		_, _, err := readEventFrame(bytes.NewReader(c.data))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	if _, _, err := readEventFrame(bytes.NewReader(nil)); err != io.EOF {
		t.Errorf("an empty stream is EOF, got %v", err)
	}
}

func TestEventHeaderTypes(t *testing.T) {
	str := func(name, v string) []byte {
		b := []byte{byte(len(name))}
		b = append(b, name...)
		b = append(b, 7, byte(len(v)>>8), byte(len(v)))
		return append(b, v...)
	}
	typed := func(name string, typ byte, size int) []byte {
		b := []byte{byte(len(name))}
		b = append(b, name...)
		b = append(b, typ)
		return append(b, make([]byte, size)...)
	}
	var all []byte
	all = append(all, typed("t", 0, 0)...)
	all = append(all, typed("f", 1, 0)...)
	all = append(all, typed("b", 2, 1)...)
	all = append(all, typed("s", 3, 2)...)
	all = append(all, typed("i", 4, 4)...)
	all = append(all, typed("l", 5, 8)...)
	all = append(all, typed("ts", 8, 8)...)
	all = append(all, typed("u", 9, 16)...)
	all = append(all, append([]byte{2, 'b', 'y', 6, 0, 2}, 1, 2)...) // bytes
	all = append(all, str("k", "v")...)
	got, err := parseEventHeaders(all)
	if err != nil || len(got) != 1 || got["k"] != "v" {
		t.Fatalf("headers %v %v", got, err)
	}
	bad := map[string][]byte{
		"name runs out":        {5, 'a'},
		"unknown type":         {1, 'a', 99},
		"length runs out":      {1, 'a', 7, 0},
		"value runs out":       {1, 'a', 7, 0, 9, 'x'},
		"fixed value runs out": {1, 'a', 4, 0, 0},
	}
	for name, b := range bad {
		if _, err := parseEventHeaders(b); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func covVertexKey(t *testing.T, mut func(map[string]string)) string {
	t.Helper()
	pk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(pk)
	m := map[string]string{"type": "service_account", "client_email": "b@x", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))}
	if mut != nil {
		mut(m)
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestVertexTokensRefuseBadKeys(t *testing.T) {
	ctx := context.Background()
	if _, err := newVertexTokens("", nil).get(ctx); err == nil || !strings.Contains(err.Error(), "no key") {
		t.Errorf("no key: %v", err)
	}
	if tok, err := newVertexTokens(" plain-token ", nil).get(ctx); err != nil || tok != "plain-token" {
		t.Errorf("a plain token is used as it is: %q %v", tok, err)
	}
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ecDER, _ := x509.MarshalPKCS8PrivateKey(ec)
	pkcs1 := x509.MarshalPKCS1PrivateKey(mustRSA(t))
	cases := map[string]struct {
		key  string
		want string
	}{
		"not json":     {`{nope`, "not valid JSON"},
		"unknown type": {`{"type":"user"}`, "unknown key type"},
		"no private":   {covVertexKey(t, func(m map[string]string) { m["private_key"] = "" }), "no private_key"},
		"unreadable": {covVertexKey(t, func(m map[string]string) {
			m["private_key"] = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("junk")}))
		}), "can not be read"},
		"not rsa": {covVertexKey(t, func(m map[string]string) {
			m["private_key"] = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecDER}))
		}), "not an RSA key"},
		"foreign token": {covVertexKey(t, func(m map[string]string) { m["token_uri"] = "https://evil.example/t" }), "not a Google https"},
		"user foreign":  {`{"type":"authorized_user","client_id":"i","client_secret":"s","refresh_token":"r","token_uri":"https://evil.example"}`, ""},
	}
	old := vertexTokenURLOK
	defer func() { vertexTokenURLOK = old }()
	for name, c := range cases {
		vertexTokenURLOK = old
		if name == "user foreign" {
			vertexTokenURLOK = func(string) bool { return false } // the refresh flow always uses Google's address
			c.want = "not a Google https"
		}
		_, err := newVertexTokens(c.key, http.DefaultClient).get(ctx)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
	// PKCS#1 keys work
	k := map[string]string{"type": "service_account", "client_email": "b@x", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: pkcs1}))}
	if _, err := vertexAssertion(vertexKey{Type: k["type"], ClientEmail: k["client_email"], PrivateKey: k["private_key"]}, "aud", time.Now()); err != nil {
		t.Errorf("pkcs1: %v", err)
	}
}

func mustRSA(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	pk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

func TestVertexTokenExchangeAnswers(t *testing.T) {
	old := vertexTokenURLOK
	vertexTokenURLOK = func(string) bool { return true }
	defer func() { vertexTokenURLOK = old }()
	status, body := 200, ""
	var form string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.Form.Encode()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	ctx := context.Background()
	key := covVertexKey(t, func(m map[string]string) { m["token_uri"] = srv.URL })
	status, body = 401, `{"error":"denied"}`
	if _, err := newVertexTokens(key, srv.Client()).get(ctx); err == nil {
		t.Error("a refused exchange must fail")
	}
	status, body = 200, `{"nothing":1}`
	if _, err := newVertexTokens(key, srv.Client()).get(ctx); err == nil || !strings.Contains(err.Error(), "no access_token") {
		t.Errorf("no token: %v", err)
	}
	// a token without a lifetime lasts an hour and is reused
	now := time.Now()
	v := newVertexTokens(key, srv.Client())
	v.now = func() time.Time { return now }
	status, body = 200, `{"access_token":"ya29.abcdefghijklmnop0123456789"}`
	tok, err := v.get(ctx)
	if err != nil || tok == "" {
		t.Fatal(tok, err)
	}
	if !v.expires.Equal(now.Add(time.Hour)) {
		t.Errorf("expires %v", v.expires)
	}
	body = `{}`
	if again, err := v.get(ctx); err != nil || again != tok {
		t.Errorf("a fresh token is reused: %q %v", again, err)
	}
	now = now.Add(2 * time.Hour)
	if _, err := v.get(ctx); err == nil {
		t.Error("an old token is exchanged again")
	}
	// the refresh-token flow
	vertexTokenURLOK = func(u string) bool { return u == vertexDefaultTokenURL }
	rt := newVertexTokens(`{"type":"authorized_user","client_id":"cid","client_secret":"csecret-0123456789","refresh_token":"rtok-0123456789"}`, &http.Client{Transport: redirectTo(srv.URL)})
	status, body = 200, `{"access_token":"ya29.refreshedtoken0123456789","expires_in":60}`
	if tok, err := rt.get(ctx); err != nil || !strings.HasPrefix(tok, "ya29.refreshed") {
		t.Fatalf("refresh: %q %v", tok, err)
	}
	if !strings.Contains(form, "grant_type=refresh_token") || !strings.Contains(form, "client_id=cid") {
		t.Errorf("form %s", form)
	}
}

// redirectTo sends every request to base (a test server).
func redirectTo(base string) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		u := *r.URL
		target, _ := http.NewRequest(r.Method, base+u.Path, r.Body)
		target.Header = r.Header
		return http.DefaultTransport.RoundTrip(target.WithContext(r.Context()))
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
