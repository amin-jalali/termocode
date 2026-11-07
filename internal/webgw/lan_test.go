//go:build unix

package webgw

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Spec (LAN mode): only private RFC 1918 IPv4 addresses are LAN
// addresses. Loopback, link-local, CGNAT (Tailscale's 100.64/10),
// public, wildcard and IPv6 addresses are not.
func TestIsPrivateLAN(t *testing.T) {
	yes := []string{"192.168.1.20", "192.168.0.1", "10.0.0.5", "10.255.1.2", "172.16.0.1", "172.31.255.254"}
	no := []string{"8.8.8.8", "127.0.0.1", "0.0.0.0", "255.255.255.255", "169.254.1.1", "100.64.0.1",
		"172.15.0.1", "172.32.0.1", "192.169.1.1", "::1", "fe80::1", "fd00::1", "2001:db8::1"}
	for _, s := range yes {
		if !IsPrivateLAN(net.ParseIP(s)) {
			t.Errorf("%s: not private", s)
		}
	}
	for _, s := range no {
		if IsPrivateLAN(net.ParseIP(s)) {
			t.Errorf("%s: private", s)
		}
	}
	if IsPrivateLAN(nil) {
		t.Error("nil: private")
	}
}

// Spec (LAN mode): the gateway picks one private address of an
// interface that is up; container and VPN bridges are skipped, and
// 192.168/16 comes before 10/8 before 172.16/12.
func TestPickLANAddr(t *testing.T) {
	ip := func(s string) net.IP { return net.ParseIP(s) }
	ifs := []LANIface{
		{Name: "lo", Up: true, Addrs: []net.IP{ip("127.0.0.1")}},
		{Name: "docker0", Up: true, Addrs: []net.IP{ip("172.17.0.1")}},
		{Name: "br-1234", Up: true, Addrs: []net.IP{ip("192.168.99.1")}},
		{Name: "tailscale0", Up: true, Addrs: []net.IP{ip("100.101.1.2")}},
		{Name: "eth0", Up: false, Addrs: []net.IP{ip("192.168.5.5")}},
		{Name: "wlp2s0", Up: true, Addrs: []net.IP{ip("10.1.2.3"), ip("fe80::1"), ip("192.168.1.20")}},
	}
	got, ok := PickLANAddr(ifs)
	if !ok || !got.Equal(ip("192.168.1.20")) {
		t.Fatalf("picked %v %v", got, ok)
	}
	if _, ok := PickLANAddr(ifs[:5]); ok {
		t.Fatal("picked an address from a bridge, a VPN or a down interface")
	}
	if _, ok := PickLANAddr([]LANIface{{Name: "eth0", Up: true, Addrs: []net.IP{ip("8.8.8.8")}}}); ok {
		t.Fatal("picked a public address")
	}
}

// Spec (LAN mode): the certificate is made once, its key is 0600 in a
// 0700 folder, and the next start reuses it (same fingerprint), so a
// paired tablet's pin keeps working. The fingerprint is the base64url
// SHA-256 of the certificate (43 characters).
func TestLANCertCreateAndReuse(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lan")
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	c1, fp1, err := LoadOrCreateLANCert(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(c1.Certificate[0])
	if fp1 != base64.RawURLEncoding.EncodeToString(sum[:]) || len(fp1) != 43 {
		t.Fatalf("fingerprint %q", fp1)
	}
	if fp1 != CertFingerprint(c1.Certificate[0]) {
		t.Fatal("CertFingerprint differs")
	}
	fi, err := os.Stat(dir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("folder %v %v", fi, err)
	}
	for _, f := range []string{lanCertFile, lanKeyFile} {
		fi, err := os.Stat(filepath.Join(dir, f))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", f, fi, err)
		}
	}
	leaf, err := x509.ParseCertificate(c1.Certificate[0])
	if err != nil || leaf.IsCA || !leaf.NotAfter.After(now.AddDate(5, 0, 0)) {
		t.Fatalf("leaf %v", err)
	}
	c2, fp2, err := LoadOrCreateLANCert(dir, now.Add(time.Hour))
	if err != nil || fp2 != fp1 || !bytes.Equal(c2.Certificate[0], c1.Certificate[0]) {
		t.Fatalf("not reused: %q vs %q (%v)", fp2, fp1, err)
	}
	// A damaged pair is made new, never half used.
	if err := os.WriteFile(filepath.Join(dir, lanKeyFile), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, fp3, err := LoadOrCreateLANCert(dir, now)
	if err != nil || fp3 == fp1 {
		t.Fatalf("damaged key: %q %v", fp3, err)
	}
	// A key file other users can read is refused, not used.
	if err := os.Chmod(filepath.Join(dir, lanKeyFile), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrCreateLANCert(dir, now); err == nil {
		t.Fatal("a 0644 key was used")
	}
}

// Spec (LAN mode): ListenLAN binds only a private LAN address, never a
// wildcard, loopback or public one.
func TestListenLANRefusesNonPrivate(t *testing.T) {
	cert, _, err := LoadOrCreateLANCert(filepath.Join(t.TempDir(), "lan"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"0.0.0.0", "127.0.0.1", "8.8.8.8", "100.64.0.1", "::", "fe80::1"} {
		ln, _, err := ListenLAN(net.ParseIP(s), 0, cert)
		if err == nil {
			ln.Close()
			t.Errorf("%s: listened", s)
		} else if !errors.Is(err, ErrNotPrivateLAN) {
			t.Errorf("%s: %v", s, err)
		}
	}
	if _, _, err := ListenLAN(nil, 0, cert); !errors.Is(err, ErrNotPrivateLAN) {
		t.Errorf("nil: %v", err)
	}
}

// Spec (LAN mode): on a real private address of this machine (skipped
// when it has none) the listener speaks TLS with the stored certificate,
// and the site is https://<ip>:<port>.
func TestListenLANServesTLS(t *testing.T) {
	ip, ok := PickLANAddr(LANAddrs())
	if !ok {
		t.Skip("no private LAN address here")
	}
	cert, fp, err := LoadOrCreateLANCert(filepath.Join(t.TempDir(), "lan"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ln, site, err := ListenLAN(ip, 0, cert)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	host := net.JoinHostPort(ip.String(), strconv.Itoa(port))
	if site.Scheme != "https" || len(site.Hosts) != 1 || site.Hosts[0] != host {
		t.Fatalf("site %+v", site)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }), ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()
	conn, err := tls.Dial("tcp", host, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) //nolint:gosec // the pin is checked below
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	got := CertFingerprint(conn.ConnectionState().PeerCertificates[0].Raw)
	if got != fp {
		t.Fatalf("served %q, stored %q", got, fp)
	}
}

// Spec (LAN mode): with no passkeys (an IP address has no name for
// them) the passkey sign-in says so instead of crashing.
func TestLoginWithoutPasskeys(t *testing.T) {
	l := NewLogin(LoginConfig{Tokens: NewTokens(time.Minute, time.Hour), Now: time.Now})
	for _, path := range []string{"/login/start", "/login/finish"} {
		body := `{}`
		if path == "/login/finish" {
			body = `{"login_id":"x","credential":{}}`
		}
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		l.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "no_passkeys") {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	var pk *Passkeys
	if _, _, err := pk.BeginLogin(); !errors.Is(err, ErrNoPasskeys) {
		t.Fatalf("nil BeginLogin: %v", err)
	}
	if _, err := pk.FinishLogin("x", nil); !errors.Is(err, ErrNoPasskeys) {
		t.Fatalf("nil FinishLogin: %v", err)
	}
}
