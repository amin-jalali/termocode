package dbclient

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
)

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestCA(t *testing.T, name string) testCA {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return testCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// issue signs a leaf for dns (server) or a client cert; it returns the
// cert and key PEM.
func (ca testCA) issue(t *testing.T, dns string, client bool) ([]byte, []byte) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: dns},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if client {
		tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	} else {
		tpl.DNSNames = []string{dns}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	kder, _ := x509.MarshalECPrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder})
}

func writeTLSFile(t *testing.T, dir, name string, data []byte, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, mode); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(p, mode)
	return p
}

// handshake runs a TLS server with serverCfg on one end of a pipe and
// the client config on the other.
func handshake(t *testing.T, client *tls.Config, server *tls.Config) error {
	t.Helper()
	a, b := dbtest.BufferedPipe()
	defer a.Close()
	defer b.Close()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s := tls.Server(b, server)
		_ = s.SetDeadline(time.Now().Add(5 * time.Second))
		if s.Handshake() == nil {
			_, _ = s.Write([]byte{1})
		}
		_ = b.Close()
	}()
	c := tls.Client(a, client)
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	err := c.Handshake()
	if err == nil {
		// TLS 1.3 reports a refused client cert on the first read.
		_, err = c.Read(make([]byte, 1))
	}
	_ = a.Close()
	wg.Wait()
	return err
}

func serverTLS(t *testing.T, ca testCA, dns string, clientCAs *x509.CertPool) *tls.Config {
	t.Helper()
	certPEM, keyPEM := ca.issue(t, dns, false)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	if clientCAs != nil {
		cfg.ClientCAs = clientCAs
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg
}

func TestTLSModes(t *testing.T) {
	dir := t.TempDir()
	ca, other := newTestCA(t, "Test CA"), newTestCA(t, "Other CA")
	caFile := writeTLSFile(t, dir, "ca.pem", ca.pem, 0o644)
	good := serverTLS(t, ca, "db.internal", nil)
	wrongName := serverTLS(t, ca, "elsewhere.internal", nil)
	foreign := serverTLS(t, other, "db.internal", nil)

	cases := []struct {
		name   string
		ssl    SSLConfig
		server *tls.Config
		ok     bool
	}{
		{"verify-full ok", SSLConfig{Mode: "verify-full", CA: caFile}, good, true},
		{"verify-full wrong name", SSLConfig{Mode: "verify-full", CA: caFile}, wrongName, false},
		{"verify-full other CA", SSLConfig{Mode: "verify-full", CA: caFile}, foreign, false},
		{"empty mode is verify-full", SSLConfig{CA: caFile}, wrongName, false},
		{"empty mode ok", SSLConfig{CA: caFile}, good, true},
		{"empty mode without CA uses system roots", SSLConfig{}, good, false},
		{"verify-ca ignores the name", SSLConfig{Mode: "verify-ca", CA: caFile}, wrongName, true},
		{"verify-ca other CA", SSLConfig{Mode: "verify-ca", CA: caFile}, foreign, false},
		{"require with a CA verifies the chain", SSLConfig{Mode: "require", CA: caFile}, foreign, false},
		{"require without a CA only encrypts", SSLConfig{Mode: "require"}, foreign, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := TLSConfig(&tc.ssl, "db.internal")
			if err != nil {
				t.Fatal(err)
			}
			if cfg == nil {
				t.Fatal("no TLS config")
			}
			err = handshake(t, cfg, tc.server)
			if (err == nil) != tc.ok {
				t.Fatalf("handshake err = %v, want ok=%v", err, tc.ok)
			}
		})
	}
	if cfg, err := TLSConfig(&SSLConfig{Mode: "disable"}, "db"); err != nil || cfg != nil {
		t.Fatalf("disable: %v %v", cfg, err)
	}
	if cfg, err := TLSConfig(nil, "db"); err != nil || cfg != nil {
		t.Fatalf("nil: %v %v", cfg, err)
	}
	if _, err := TLSConfig(&SSLConfig{Mode: "sometimes"}, "db"); err == nil {
		t.Fatal("unknown mode accepted")
	}
	if _, err := TLSConfig(&SSLConfig{Mode: "verify-full", CA: writeTLSFile(t, dir, "empty.pem", []byte("nothing"), 0o644)}, "db"); err == nil || !strings.Contains(err.Error(), "no certificate") {
		t.Fatalf("empty CA: %v", err)
	}
}

func TestTLSClientCertificate(t *testing.T) {
	dir := t.TempDir()
	ca := newTestCA(t, "Test CA")
	caFile := writeTLSFile(t, dir, "ca.pem", ca.pem, 0o644)
	certPEM, keyPEM := ca.issue(t, "app", true)
	certFile := writeTLSFile(t, dir, "client.pem", certPEM, 0o644)
	openKey := writeTLSFile(t, dir, "client-open.key", keyPEM, 0o644)
	key := writeTLSFile(t, dir, "client.key", keyPEM, 0o600)
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	server := serverTLS(t, ca, "db.internal", pool)

	if _, err := TLSConfig(&SSLConfig{Mode: "verify-full", CA: caFile, Cert: certFile, Key: openKey}, "db.internal"); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("group-readable key: err = %v", err)
	}
	if _, err := TLSConfig(&SSLConfig{Mode: "verify-full", CA: caFile, Cert: certFile}, "db.internal"); err == nil {
		t.Fatal("a cert without a key was accepted")
	}
	cfg, err := TLSConfig(&SSLConfig{Mode: "verify-full", CA: caFile, Cert: certFile, Key: key}, "db.internal")
	if err != nil {
		t.Fatal(err)
	}
	if err := handshake(t, cfg, server); err != nil {
		t.Fatalf("with client cert: %v", err)
	}
	noCert, _ := TLSConfig(&SSLConfig{Mode: "verify-full", CA: caFile}, "db.internal")
	if err := handshake(t, noCert, server); err == nil {
		t.Fatal("the server took a client without a certificate")
	}
}

// recordConn answers whatever the driver sends with reply and keeps
// every byte the client wrote.
type recordConn struct {
	mu   sync.Mutex
	sent bytes.Buffer
}

func (r *recordConn) dial(reply []byte) DialFunc { return r.dialGreet(reply, false) }

// dialGreet: serverFirst sends reply before reading (MySQL greets first).
func (r *recordConn) dialGreet(reply []byte, serverFirst bool) DialFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			buf := make([]byte, 4096)
			_ = b.SetDeadline(time.Now().Add(3 * time.Second))
			first := true
			if serverFirst {
				first = false
				if _, err := b.Write(reply); err != nil {
					return
				}
			}
			for {
				n, err := b.Read(buf)
				r.mu.Lock()
				r.sent.Write(buf[:n])
				r.mu.Unlock()
				if err != nil || reply == nil {
					return
				}
				if first {
					first = false
					_, _ = b.Write(reply)
				}
			}
		}()
		return a, nil
	}
}

func (r *recordConn) bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.sent.Bytes()...)
}

func TestTLSRequiredPostgresNeverFallsBackToPlaintext(t *testing.T) {
	rec := &recordConn{}
	// The server answers the SSLRequest with 'N' (no TLS).
	c := Conn{ID: "pg", Driver: "postgres", Host: "db.internal", Port: 5432, User: "app", Database: "app", SSL: &SSLConfig{Mode: "require"}, Dial: rec.dial([]byte("N"))}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s, err := NewSQLSession(ctx, c, "pg-secret-pw")
	if err == nil {
		_ = s.Close()
		t.Fatal("connected without TLS")
	}
	sent := rec.bytes()
	// SSLRequest is 8 bytes: length 8, code 80877103. A startup message
	// would carry the user name in clear text.
	if bytes.Contains(sent, []byte("user")) || bytes.Contains(sent, []byte("app")) {
		t.Fatalf("a plaintext startup followed the refused TLS: %q", sent)
	}
	if len(sent) < 8 || !bytes.Equal(sent[4:8], []byte{0x04, 0xd2, 0x16, 0x2f}) {
		t.Fatalf("no SSLRequest first: %x", sent)
	}
}

func TestTLSRequiredRedisSpeaksTLSFirst(t *testing.T) {
	rec := &recordConn{}
	c := Conn{ID: "r", Driver: "redis", Host: "cache.internal", Port: 6379, SSL: &SSLConfig{Mode: "require"}, Dial: rec.dial(nil)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s, err := NewRedisSession(ctx, c, "redis-secret-pw", nil); err == nil {
		_ = s.Close()
		t.Fatal("connected to a server that never answered TLS")
	}
	sent := rec.bytes()
	if len(sent) == 0 || sent[0] != 0x16 { // TLS handshake record
		t.Fatalf("first byte %x, want a TLS ClientHello", sent)
	}
	if bytes.Contains(sent, []byte("redis-secret-pw")) || bytes.Contains(sent, []byte("AUTH")) {
		t.Fatal("AUTH went out in clear text")
	}
	// rediss:// with ssl mode disable is a contradiction, not a downgrade.
	c2 := Conn{ID: "r", Driver: "redis", URL: "rediss://cache.internal:6380/0", SSL: &SSLConfig{Mode: "disable"}, Dial: rec.dial(nil)}
	if _, err := NewRedisSession(ctx, c2, "", nil); err == nil || !strings.Contains(err.Error(), "disable") {
		t.Fatalf("rediss + disable: err = %v", err)
	}
}

func TestTLSRequiredMySQLSendsNoPassword(t *testing.T) {
	rec := &recordConn{}
	// A MySQL server greeting without CLIENT_SSL: the driver must stop.
	c := Conn{ID: "my", Driver: "mysql", Host: "db.internal", Port: 3306, User: "app", Database: "app", SSL: &SSLConfig{Mode: "require"}, Dial: rec.dialGreet(mysqlGreeting(), true)}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s, err := NewSQLSession(ctx, c, "my-secret-pw")
	if err == nil {
		_ = s.Close()
		t.Fatal("connected without TLS")
	}
	if !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("err = %v, want the no-TLS refusal", err)
	}
	if bytes.Contains(rec.bytes(), []byte("app")) {
		t.Fatalf("the login went out in clear text: %q", rec.bytes())
	}
}

// mysqlGreeting is a HandshakeV10 packet whose capabilities lack
// CLIENT_SSL.
func mysqlGreeting() []byte {
	var p bytes.Buffer
	p.WriteByte(10)
	p.WriteString("8.0.0-test\x00")
	p.Write([]byte{1, 0, 0, 0})          // connection id
	p.WriteString("abcdefgh")            // auth data part 1
	p.WriteByte(0)                       // filler
	caps := uint32(0x0000_0200 | 0x8000) // PROTOCOL_41 | SECURE_CONNECTION, no SSL (0x800)
	p.Write([]byte{byte(caps), byte(caps >> 8)})
	p.WriteByte(33)       // charset
	p.Write([]byte{2, 0}) // status
	p.Write([]byte{byte(caps >> 16), byte(caps >> 24)})
	p.WriteByte(21)
	p.Write(make([]byte, 10))
	p.WriteString("ijklmnopqrst\x00")
	body := p.Bytes()
	head := []byte{byte(len(body)), byte(len(body) >> 8), byte(len(body) >> 16), 0}
	return append(head, body...)
}
