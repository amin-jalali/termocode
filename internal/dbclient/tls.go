package dbclient

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
)

// SSL modes of the "ssl" object (libpq names; MySQL and Redis use the
// same ones).
const (
	SSLDisable    = "disable"
	SSLRequire    = "require"
	SSLVerifyCA   = "verify-ca"
	SSLVerifyFull = "verify-full"
)

// SSLModes is the order the mode picker shows.
var SSLModes = []string{SSLDisable, SSLRequire, SSLVerifyCA, SSLVerifyFull}

// EffectiveSSLMode is the mode an "ssl" object means: an empty mode is
// verify-full (the safe default); require with a CA file verifies the
// chain like verify-ca (as libpq does).
func EffectiveSSLMode(ssl *SSLConfig) (string, error) {
	if ssl == nil {
		return "", nil
	}
	mode := strings.ToLower(strings.TrimSpace(ssl.Mode))
	switch mode {
	case "":
		return SSLVerifyFull, nil
	case SSLDisable, SSLVerifyCA, SSLVerifyFull:
		return mode, nil
	case SSLRequire:
		if ssl.CA != "" {
			return SSLVerifyCA, nil
		}
		return SSLRequire, nil
	}
	return "", fmt.Errorf("unknown SSL mode %q (use disable, require, verify-ca or verify-full)", sanitizeLine(ssl.Mode))
}

// TLSConfig builds the client TLS config of ssl for serverName (the
// database host, not the SSH bastion). nil, nil: TLS is off (mode
// disable). The CA, cert and key paths must already be absolute.
func TLSConfig(ssl *SSLConfig, serverName string) (*tls.Config, error) {
	mode, err := EffectiveSSLMode(ssl)
	if err != nil {
		return nil, err
	}
	if ssl == nil || mode == SSLDisable {
		return nil, nil
	}
	cfg := &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12}
	var roots *x509.CertPool
	if ssl.CA != "" {
		pem, err := os.ReadFile(ssl.CA)
		if err != nil {
			return nil, fmt.Errorf("could not read the CA file %s: %s", ssl.CA, sanitizeLine(errText(err)))
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("the CA file %s holds no certificate", ssl.CA)
		}
	}
	if ssl.Cert != "" || ssl.Key != "" {
		if ssl.Cert == "" || ssl.Key == "" {
			return nil, errors.New("a client certificate needs both \"cert\" and \"key\"")
		}
		certPEM, err := os.ReadFile(ssl.Cert)
		if err != nil {
			return nil, fmt.Errorf("could not read the client certificate %s: %s", ssl.Cert, sanitizeLine(errText(err)))
		}
		keyPEM, err := readPrivateFile(ssl.Key)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("the client key %s does not exist", ssl.Key)
			}
			return nil, err
		}
		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		clear(keyPEM)
		if err != nil {
			return nil, fmt.Errorf("the client certificate and key do not load: %s", sanitizeLine(err.Error()))
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	switch mode {
	case SSLRequire:
		// Encrypted, not verified: the user chose it.
		cfg.InsecureSkipVerify = true
	case SSLVerifyCA:
		// The chain must reach the CA; the host name is not checked.
		cfg.InsecureSkipVerify = true
		cfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("the server sent no certificate")
			}
			inter := x509.NewCertPool()
			for _, c := range cs.PeerCertificates[1:] {
				inter.AddCert(c)
			}
			_, err := cs.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter})
			return err
		}
	case SSLVerifyFull:
		cfg.RootCAs = roots
	}
	return cfg, nil
}

func errText(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
