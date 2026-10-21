package containers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"
)

// RegistryConfig contains registry host names and credential helper identifiers.
// It deliberately has no field capable of holding an auth value.
type RegistryConfig struct {
	CredsStore string
	Registries []Registry
}

// ReadRegistryConfig reads a Docker-style config file without decoding auth values.
// The path is normally ~/.docker/config.json or $DOCKER_CONFIG/config.json.
func ReadRegistryConfig(path string) (RegistryConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return RegistryConfig{}, err
	}
	defer f.Close()
	return DecodeRegistryConfig(f)
}

// DecodeRegistryConfig reads the safe registry metadata from a Docker-style config.
func DecodeRegistryConfig(r io.Reader) (RegistryConfig, error) {
	d := json.NewDecoder(r)
	tok, err := d.Token()
	if err != nil {
		return RegistryConfig{}, err
	}
	if tok != json.Delim('{') {
		return RegistryConfig{}, fmt.Errorf("registry config must be an object")
	}
	cfg := RegistryConfig{}
	byHost := map[string]Registry{}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return RegistryConfig{}, err
		}
		key, ok := t.(string)
		if !ok {
			return RegistryConfig{}, fmt.Errorf("invalid config key")
		}
		switch key {
		case "credsStore":
			err = d.Decode(&cfg.CredsStore)
		case "auths":
			err = decodeHostNames(d, byHost, false)
		case "credHelpers":
			err = decodeHostNames(d, byHost, true)
		default:
			err = skipJSONValue(d)
		}
		if err != nil {
			return RegistryConfig{}, err
		}
	}
	if _, err = d.Token(); err != nil {
		return RegistryConfig{}, err
	}
	for host, registry := range byHost {
		registry.Host = host
		registry.CredsStore = cfg.CredsStore
		cfg.Registries = append(cfg.Registries, registry)
	}
	sort.Slice(cfg.Registries, func(i, j int) bool { return cfg.Registries[i].Host < cfg.Registries[j].Host })
	return cfg, nil
}

func decodeHostNames(d *json.Decoder, hosts map[string]Registry, helpers bool) error {
	tok, err := d.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('{') {
		return fmt.Errorf("registry metadata must be an object")
	}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return err
		}
		host, ok := t.(string)
		if !ok {
			return fmt.Errorf("invalid registry host")
		}
		entry := hosts[host]
		if !helpers {
			// A host in `auths` has a login (its value, often `{}` when a
			// store keeps the secret, is skipped unread).
			entry.SignedIn = true
		}
		if helpers {
			var name string
			if err := d.Decode(&name); err != nil {
				return err
			}
			entry.CredHelper = name
		} else if err := skipJSONValue(d); err != nil {
			return err
		}
		hosts[host] = entry
	}
	_, err = d.Token()
	return err
}

func skipJSONValue(d *json.Decoder) error {
	tok, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		for d.More() {
			if _, err := d.Token(); err != nil {
				return err
			}
			if err := skipJSONValue(d); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	case '[':
		for d.More() {
			if err := skipJSONValue(d); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	default:
		return nil
	}
}

// dockerHubHosts are the names the Docker Hub has in configs and refs.
var dockerHubHosts = map[string]bool{"docker.io": true, "index.docker.io": true, "registry-1.docker.io": true}

// RegistryDisplayHost is the short host of a config key or a typed host:
// "https://index.docker.io/v1/" → "docker.io", "https://ghcr.io" →
// "ghcr.io". It is for display and matching, never for commands (those
// use the config key as written).
func RegistryDisplayHost(key string) string {
	h := strings.TrimSpace(key)
	for _, p := range []string{"https://", "http://"} {
		h = strings.TrimPrefix(h, p)
	}
	if i := strings.IndexByte(h, '/'); i >= 0 {
		h = h[:i]
	}
	h = strings.ToLower(h)
	if dockerHubHosts[h] {
		return "docker.io"
	}
	return h
}

// ImageRegistryHost is the registry an image reference belongs to: the
// first path part when it looks like a host (has a dot or a port, or is
// localhost), else Docker Hub ("docker.io").
func ImageRegistryHost(ref string) string {
	name := ref
	if i := strings.IndexByte(name, '@'); i >= 0 {
		name = name[:i]
	}
	first, _, ok := strings.Cut(name, "/")
	if !ok || !(strings.ContainsAny(first, ".:") || first == "localhost") {
		return "docker.io"
	}
	return RegistryDisplayHost(first)
}

// ValidRegistryHost accepts a registry host (with an optional scheme,
// port and path); it refuses an empty value, a value that would read as
// a flag, spaces, control and shell characters.
func ValidRegistryHost(host string) error {
	if host == "" {
		return errors.New("registry host is required")
	}
	if strings.HasPrefix(host, "-") {
		return errors.New("registry host cannot start with a dash")
	}
	if len(host) > 512 {
		return errors.New("registry host is too long")
	}
	for _, r := range host {
		ok := r < unicode.MaxASCII && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(".-_:/[]", r))
		if !ok {
			return errors.New("registry host has a character that is not allowed")
		}
	}
	return nil
}

// ValidRegistryUser accepts a user name for `--username`; it refuses an
// empty value, a flag-like value, spaces and control characters.
func ValidRegistryUser(user string) error {
	if user == "" {
		return errors.New("user name is required")
	}
	if strings.HasPrefix(user, "-") {
		return errors.New("user name cannot start with a dash")
	}
	if len(user) > 256 {
		return errors.New("user name is too long")
	}
	for _, r := range user {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return errors.New("user name has a space or a control character")
		}
	}
	return nil
}
