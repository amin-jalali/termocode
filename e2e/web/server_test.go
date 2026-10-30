//go:build browser

package web

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webgw"
)

// Server is the page's gateway for one test: the real guard, security
// headers, CSP, assets and /auth of internal/webgw, with a FakeHub on /ws.
type Server struct {
	// Base is the page origin, http://127.0.0.1:<port>.
	Base   string
	Site   webgw.Site
	Hub    *FakeHub
	Tokens *webgw.Tokens
	auth   *webgw.Auth
	srv    *http.Server
}

// StartServer listens on a free loopback port and serves until the test ends.
func StartServer(t *testing.T) *Server {
	t.Helper()
	return startServer(t, false)
}

// StartServerTLS is StartServer over https with a throwaway certificate,
// as an installable (tailnet) site; the page's Chrome skips the check.
func StartServerTLS(t *testing.T) *Server {
	t.Helper()
	return startServer(t, true)
}

func startServer(t *testing.T, https bool) *Server {
	t.Helper()
	ln, site, err := webgw.Listen(webgw.ListenConfig{})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if https {
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{selfSigned(t)}, MinVersion: tls.VersionTLS12})
		site.Scheme = "https"
	}
	tokens := webgw.NewTokens(15*time.Minute, 12*time.Hour)
	s := &Server{
		Base:   site.Scheme + "://" + site.Hosts[0],
		Site:   site,
		Tokens: tokens,
		auth:   webgw.NewAuth(tokens, time.Now, nil),
	}
	s.Hub = newFakeHub(t, tokens)
	mux := http.NewServeMux()
	mux.Handle("/auth", s.auth)
	mux.Handle("/ws", s.Hub)
	if site.Installable() {
		mux.Handle(webgw.ManifestPath, webgw.Manifest())
	}
	mux.Handle("/", webgw.Assets())
	s.srv = webgw.NewServer(site, mux, webgw.DefaultLimits, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			t.Errorf("serve: %v", err)
		}
	}()
	t.Cleanup(func() {
		s.Hub.closeAll()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(ctx)
		_ = s.srv.Close()
		<-done
	})
	return s
}

// LaunchURL opens a fresh one-time link, as "Web: Start Panel" does.
func (s *Server) LaunchURL(t *testing.T) (url, secret string) {
	t.Helper()
	secret, err := s.auth.NewSecret()
	if err != nil {
		t.Fatalf("new secret: %v", err)
	}
	return s.Base + "/#s=" + secret, secret
}

// Path is the page URL with a hash route, without a secret.
func (s *Server) Path(hash string) string { return s.Base + "/" + hash }

// selfSigned makes a certificate for 127.0.0.1 that lives for one test.
func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
