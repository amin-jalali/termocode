package httpclient

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// TLS directives (`# @insecure`, `# @ca-cert file`, `# @client-cert
// file`, `# @client-key file`). Only the user's own sends read them
// (ApplyTLSDirectives); AI sends never do.
const (
	DirInsecure   = "insecure"
	DirCACert     = "ca-cert"
	DirClientCert = "client-cert"
	DirClientKey  = "client-key"
)

// ApplyTLSDirectives sets h's TLS fields from a request's directives.
// Relative paths are read from dir (the .http file's folder); "~/" is
// the home folder.
func ApplyTLSDirectives(h *Hooks, dirs map[string]string, dir string) {
	if _, ok := dirs[DirInsecure]; ok {
		h.Insecure = true
	}
	for name, dst := range map[string]*string{DirCACert: &h.CACert, DirClientCert: &h.ClientCert, DirClientKey: &h.ClientKey} {
		if v := strings.TrimSpace(dirs[name]); v != "" {
			*dst = tlsPath(v, dir)
		}
	}
}

func tlsPath(p, dir string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
		}
	}
	if !filepath.IsAbs(p) && dir != "" {
		return filepath.Join(dir, p)
	}
	return p
}

// hasTLSFiles reports whether the send needs its own TLS config.
func (h Hooks) hasTLSFiles() bool { return h.CACert != "" || h.ClientCert != "" || h.ClientKey != "" }

// tlsError is a TLS setup failure; its text never holds file content.
type tlsError struct{ msg string }

func (e *tlsError) Error() string { return e.msg }

// tlsConfig builds the send's TLS config (nil = the default).
func tlsConfig(h Hooks) (*tls.Config, error) {
	if !h.Insecure && !h.hasTLSFiles() {
		return nil, nil
	}
	cfg := &tls.Config{InsecureSkipVerify: h.Insecure} //nolint:gosec // the user's `k` retry or `# @insecure`
	if h.CACert != "" {
		b, err := os.ReadFile(h.CACert)
		if err != nil {
			return nil, &tlsError{"cannot read the CA file " + h.CACert + ": " + fileErr(err)}
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(b) {
			return nil, &tlsError{"no PEM certificate in the CA file " + h.CACert}
		}
		cfg.RootCAs = pool
	}
	if h.ClientCert != "" || h.ClientKey != "" {
		if h.ClientCert == "" {
			return nil, &tlsError{"# @client-key needs a # @client-cert file too"}
		}
		key := h.ClientKey
		if key == "" {
			key = h.ClientCert // one PEM file with both
		}
		cb, err := os.ReadFile(h.ClientCert)
		if err != nil {
			return nil, &tlsError{"cannot read the client certificate " + h.ClientCert + ": " + fileErr(err)}
		}
		kb, err := os.ReadFile(key)
		if err != nil {
			return nil, &tlsError{"cannot read the client key " + key + ": " + fileErr(err)}
		}
		pair, err := tls.X509KeyPair(cb, kb)
		if err != nil {
			// The x509 error text can quote bytes of the file: keep it short.
			return nil, &tlsError{"the client certificate or key is not a valid PEM pair (an encrypted key is not supported)"}
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	return cfg, nil
}

func fileErr(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "no such file"
	case errors.Is(err, os.ErrPermission):
		return "permission denied"
	}
	return "read failed"
}
