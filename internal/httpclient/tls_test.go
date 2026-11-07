package httpclient

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testCA makes a CA and one leaf it signs; it returns the CA (as a
// pool and as PEM) and the leaf cert and key PEM.
func testCA(t *testing.T, leafCN string, client bool) (pool *x509.CertPool, caPEM, certPEM, keyPEM []byte) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	usage := x509.ExtKeyUsageServerAuth
	if client {
		usage = x509.ExtKeyUsageClientAuth
	}
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: leafCN},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage},
		DNSNames: []string{"localhost"},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	kDER, _ := x509.MarshalECPrivateKey(key)
	pool = x509.NewCertPool()
	pool.AddCert(ca)
	return pool,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kDER})
}

func writeFile(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestApplyTLSDirectives(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var h Hooks
	ApplyTLSDirectives(&h, map[string]string{
		"insecure": "", "ca-cert": "certs/ca.pem", "client-cert": "~/me.pem", "client-key": "/abs/me.key",
	}, "/work")
	if !h.Insecure || h.CACert != filepath.Join("/work", "certs/ca.pem") ||
		h.ClientCert != filepath.Join(home, "me.pem") || h.ClientKey != "/abs/me.key" {
		t.Fatalf("hooks %+v", h)
	}
	// No directives: nothing changes.
	var none Hooks
	ApplyTLSDirectives(&none, map[string]string{"timeout": "5s"}, "/work")
	if none.Insecure || none.CACert != "" || none.ClientCert != "" || none.ClientKey != "" {
		t.Fatalf("hooks changed without TLS directives: %+v", none)
	}
}

func TestSendCustomCA(t *testing.T) {
	_, caPEM, certPEM, keyPEM := testCA(t, "localhost", false)
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	srv.StartTLS()
	defer srv.Close()
	c := New(Options{})
	if r := send(t, c, "GET", srv.URL, "", Hooks{}); r.ErrKind != ErrKindTLS {
		t.Fatalf("an unknown CA must fail: %v %q", r.ErrKind, r.Err)
	}
	ca := writeFile(t, t.TempDir(), "ca.pem", caPEM)
	r := send(t, c, "GET", srv.URL, "", Hooks{CACert: ca})
	if r.Status != 200 || string(r.Body) != "ok" {
		t.Fatalf("custom CA: %d %q %s", r.Status, r.Body, r.Err)
	}
	// The CA is per send.
	if r := send(t, c, "GET", srv.URL, "", Hooks{}); r.ErrKind != ErrKindTLS {
		t.Fatalf("the CA must not stick: %v", r.ErrKind)
	}
}

func TestSendClientCert(t *testing.T) {
	pool, _, certPEM, keyPEM := testCA(t, "me", true)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello "+r.TLS.PeerCertificates[0].Subject.CommonName)
	}))
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}
	srv.StartTLS()
	defer srv.Close()
	dir := t.TempDir()
	cert := writeFile(t, dir, "me.crt", certPEM)
	key := writeFile(t, dir, "me.key", keyPEM)
	c := New(Options{})
	if r := send(t, c, "GET", srv.URL, "", Hooks{Insecure: true}); r.Err == "" {
		t.Fatalf("no client cert must fail, got %d", r.Status)
	}
	r := send(t, c, "GET", srv.URL, "", Hooks{Insecure: true, ClientCert: cert, ClientKey: key})
	if r.Status != 200 || string(r.Body) != "hello me" {
		t.Fatalf("client cert: %d %q %s", r.Status, r.Body, r.Err)
	}
	// One PEM file with both the cert and the key.
	both := writeFile(t, dir, "me.pem", append(append([]byte{}, certPEM...), keyPEM...))
	r = send(t, c, "GET", srv.URL, "", Hooks{Insecure: true, ClientCert: both})
	if r.Status != 200 {
		t.Fatalf("combined PEM: %d %s", r.Status, r.Err)
	}
}

func TestSendTLSFileErrors(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	dir := t.TempDir()
	bad := writeFile(t, dir, "bad.pem", []byte("not a pem "+"sk-"+"test-secret"))
	c := New(Options{})
	for name, h := range map[string]Hooks{
		"missing CA":     {CACert: filepath.Join(dir, "nope.pem")},
		"bad CA":         {CACert: bad},
		"missing cert":   {ClientCert: filepath.Join(dir, "nope.crt")},
		"bad cert + key": {ClientCert: bad, ClientKey: bad},
	} {
		r := send(t, c, "GET", srv.URL, "", h)
		if r.ErrKind != ErrKindTLS || !strings.HasPrefix(r.Err, "TLS: ") {
			t.Errorf("%s: kind %v err %q", name, r.ErrKind, r.Err)
		}
		if strings.Contains(r.Err, "sk-"+"test") {
			t.Errorf("%s: the file's content leaked into the error: %q", name, r.Err)
		}
	}
}
