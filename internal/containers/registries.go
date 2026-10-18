package containers

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
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
