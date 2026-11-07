//go:build unix

package webgw

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// LAN mode ("web_listen": "lan"): a tablet on the same Wi-Fi reaches the
// gateway with no Tailscale and no cloud. The gateway listens on one
// private (RFC 1918) IPv4 address of this machine only, never a
// wildcard, with https on a self-signed certificate it makes once and
// keeps (0600) in <devices>/lan. The key never leaves the laptop. The
// pairing link carries the certificate's SHA-256 fingerprint, and the
// app trusts that exact certificate for that origin, nothing else.
// Passkeys need a host name, so a LAN site has none: a paired device
// signs in again with its remembered-browser key (or the app key).

// ErrNotPrivateLAN: the address is not a private LAN address.
var ErrNotPrivateLAN = errors.New("LAN mode listens only on a private address (192.168.x.x, 10.x.x.x or 172.16-31.x.x)")

// ErrNoLAN: this machine has no private LAN address on an interface that
// is up.
var ErrNoLAN = errors.New("this computer has no Wi-Fi or LAN address (192.168.x.x, 10.x.x.x or 172.16-31.x.x). Join a network, or use \"web_listen\": \"loopback\"")

// LANConfig is LAN mode's setup.
type LANConfig struct {
	// CertDir holds the certificate and its key (0700 folder, 0600 files).
	CertDir string
	// IP is the address to listen on (nil: PickLANAddr(LANAddrs())).
	IP net.IP
}

// IsPrivateLAN reports whether ip is a private RFC 1918 IPv4 address
// (10/8, 172.16/12, 192.168/16). No IPv6, no loopback, no CGNAT.
func IsPrivateLAN(ip net.IP) bool {
	return ip.To4() != nil && ip.IsPrivate()
}

// LANIface is one network interface as PickLANAddr sees it.
type LANIface struct {
	Name  string
	Up    bool
	Addrs []net.IP
}

// lanSkip are interface name prefixes that are never the Wi-Fi or LAN:
// container and VM bridges, VPNs.
var lanSkip = []string{"docker", "br-", "veth", "virbr", "vmnet", "vboxnet", "tailscale", "tun", "tap", "wg", "utun", "lxc", "lxd", "cni", "flannel", "podman", "zt"}

// PickLANAddr picks the address the tablet reaches: a private address of
// an interface that is up and not a bridge or VPN; 192.168/16 first,
// then 10/8, then 172.16/12.
func PickLANAddr(ifs []LANIface) (net.IP, bool) {
	rank := func(ip net.IP) int {
		v4 := ip.To4()
		switch {
		case v4[0] == 192:
			return 0
		case v4[0] == 10:
			return 1
		}
		return 2
	}
	var best net.IP
	for _, f := range ifs {
		if !f.Up || skipIface(f.Name) {
			continue
		}
		for _, ip := range f.Addrs {
			if !IsPrivateLAN(ip) {
				continue
			}
			if best == nil || rank(ip) < rank(best) {
				best = ip.To4()
			}
		}
	}
	return best, best != nil
}

func skipIface(name string) bool {
	if name == "lo" {
		return true
	}
	for _, p := range lanSkip {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// LANAddrs lists this machine's interfaces and their addresses.
func LANAddrs() []LANIface {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []LANIface
	for _, f := range ifs {
		li := LANIface{Name: f.Name, Up: f.Flags&net.FlagUp != 0 && f.Flags&net.FlagLoopback == 0}
		addrs, _ := f.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				li.Addrs = append(li.Addrs, ipn.IP)
			}
		}
		out = append(out, li)
	}
	return out
}

// The certificate files in the cert folder.
const (
	lanCertFile = "cert.pem"
	lanKeyFile  = "key.pem"
)

// lanCertYears is how long the certificate is valid: the pin, not the
// date, is what the app checks, and a new certificate means pairing again.
const lanCertYears = 10

// CertFingerprint is the base64url (no padding) SHA-256 of a DER
// certificate: what the pairing link carries as fp.
func CertFingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// LoadOrCreateLANCert loads the stored certificate, or makes and stores a
// new one when there is none or the stored pair is damaged. A key file
// that others may read, or a folder that is not private, is an error.
func LoadOrCreateLANCert(dir string, now time.Time) (tls.Certificate, string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, "", err
	}
	if err := privateDir(dir); err != nil {
		return tls.Certificate{}, "", err
	}
	certPath, keyPath := filepath.Join(dir, lanCertFile), filepath.Join(dir, lanKeyFile)
	if c, err := loadLANCert(certPath, keyPath); err == nil {
		return c, CertFingerprint(c.Certificate[0]), nil
	} else if errors.Is(err, errLANKeyOpen) {
		return tls.Certificate{}, "", err
	}
	certPEM, keyPEM, err := newLANCert(now)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	if err := writePrivate(keyPath, keyPEM); err != nil {
		return tls.Certificate{}, "", err
	}
	if err := writePrivate(certPath, certPEM); err != nil {
		return tls.Certificate{}, "", err
	}
	c, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	return c, CertFingerprint(c.Certificate[0]), nil
}

// errLANKeyOpen: the key file exists but other users may read it.
var errLANKeyOpen = errors.New("the LAN key file must be 0600 and yours")

func loadLANCert(certPath, keyPath string) (tls.Certificate, error) {
	fi, err := os.Lstat(keyPath)
	if err != nil {
		return tls.Certificate{}, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 || !ok || int(st.Uid) != os.Getuid() {
		return tls.Certificate{}, fmt.Errorf("%s: %w", keyPath, errLANKeyOpen)
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

// writePrivate writes data to path (0600) through a temporary file.
func writePrivate(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// newLANCert makes a self-signed ECDSA P-256 server certificate.
func newLANCert(now time.Time) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, err
	}
	host, _ := os.Hostname()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "termocode LAN " + cleanAuditText(host)},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(lanCertYears, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip, ok := PickLANAddr(LANAddrs()); ok {
		tmpl.IPAddresses = []net.IP{ip}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	kder, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kder}), nil
}

// LANSite is the site of a LAN gateway: https on ip:port.
func LANSite(ip net.IP, port int) Site {
	return Site{Scheme: "https", Hosts: []string{net.JoinHostPort(ip.String(), strconv.Itoa(port))}}
}

// ListenLAN listens with TLS on ip:port, a private LAN address only. The
// peers are other devices, so there is no uid check: the pairing secret,
// the owner's yes on the laptop and the session token guard it.
func ListenLAN(ip net.IP, port int, cert tls.Certificate) (net.Listener, Site, error) {
	if !IsPrivateLAN(ip) {
		return nil, Site{}, ErrNotPrivateLAN
	}
	if port != 0 && (port < 1024 || port > 65535) {
		return nil, Site{}, fmt.Errorf("web port %d: use 0 or 1024..65535", port)
	}
	v4 := ip.To4()
	ln, err := net.Listen("tcp4", net.JoinHostPort(v4.String(), strconv.Itoa(port)))
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return nil, Site{}, fmt.Errorf("web port %d: %w", port, ErrPortBusy)
		}
		return nil, Site{}, err
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || !addr.IP.Equal(v4) {
		_ = ln.Close()
		return nil, Site{}, ErrNotPrivateLAN
	}
	tl := tls.NewListener(ln, &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"http/1.1"},
	})
	return tl, LANSite(v4, addr.Port), nil
}
