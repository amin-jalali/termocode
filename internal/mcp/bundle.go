package mcp

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// MCP bundles (MCPX-31): a .mcpb file is a zip with manifest.json and
// the server's files. termocode reads the manifest, shows it, asks the
// user_config values (sensitive ones go to the secret store as
// ${secret:}), unpacks the files into its data folder and adds one
// mcp.json entry that runs the bundle from there.

// Limits for one bundle.
const (
	bundleMaxFiles = 20000
	bundleMaxBytes = 512 << 20
)

// BundleManifest is the part of manifest.json termocode uses.
type BundleManifest struct {
	ManifestVersion string `json:"manifest_version"`
	DXTVersion      string `json:"dxt_version"`
	Name            string `json:"name"`
	DisplayName     string `json:"display_name"`
	Version         string `json:"version"`
	Description     string `json:"description"`
	Author          struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"author"`
	Server struct {
		Type       string `json:"type"`
		EntryPoint string `json:"entry_point"`
		MCPConfig  struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcp_config"`
	} `json:"server"`
	UserConfig map[string]BundleField `json:"user_config"`
	Tools      []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"tools"`
}

// BundleField is one user_config value the bundle asks for.
type BundleField struct {
	Type        string `json:"type"` // string, number, boolean, directory, file
	Title       string `json:"title"`
	Description string `json:"description"`
	Sensitive   bool   `json:"sensitive"`
	Required    bool   `json:"required"`
	Default     any    `json:"default"`
}

// DefaultText is the field's default as text ("" none).
func (f BundleField) DefaultText() string {
	switch v := f.Default.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// Bundle is an opened .mcpb file.
type Bundle struct {
	Path     string
	Manifest BundleManifest
}

// OpenBundle reads a .mcpb's manifest.
func OpenBundle(p string) (*Bundle, error) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, fmt.Errorf("not a bundle (.mcpb is a zip file): %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "manifest.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		var m BundleManifest
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("manifest.json: %w", err)
		}
		if m.Name == "" || m.Server.MCPConfig.Command == "" {
			return nil, errors.New("manifest.json needs a name and server.mcp_config.command")
		}
		return &Bundle{Path: p, Manifest: m}, nil
	}
	return nil, errors.New("the bundle has no manifest.json")
}

// ServerName is the mcp.json name of the bundle.
func (b *Bundle) ServerName() string {
	n := strings.ToLower(b.Manifest.Name)
	n = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, n)
	return strings.Trim(n, "-")
}

// ConfigKeys lists the user_config keys, sorted.
func (b *Bundle) ConfigKeys() []string {
	keys := make([]string, 0, len(b.Manifest.UserConfig))
	for k := range b.Manifest.UserConfig {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SecretName is the ${secret:} name of a sensitive key.
func (b *Bundle) SecretName(key string) string { return secretRef(b.ServerName(), key) }

// Secrets lists the secret names of the sensitive keys.
func (b *Bundle) Secrets() []string {
	var out []string
	for _, k := range b.ConfigKeys() {
		if b.Manifest.UserConfig[k].Sensitive {
			out = append(out, b.SecretName(k))
		}
	}
	return out
}

// Install unpacks the bundle into root/<name>-<version> (replaced when
// there) and returns that folder. Paths that leave it, links and too
// big bundles are refused.
func (b *Bundle) Install(root string) (string, error) {
	zr, err := zip.OpenReader(b.Path)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	if len(zr.File) > bundleMaxFiles {
		return "", fmt.Errorf("the bundle has too many files (%d)", len(zr.File))
	}
	ver := b.Manifest.Version
	if ver == "" {
		ver = "0"
	}
	dest := filepath.Join(root, b.ServerName()+"-"+strings.ReplaceAll(ver, "/", "_"))
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	var total int64
	for _, f := range zr.File {
		name := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return "", fmt.Errorf("the bundle has an unsafe path: %s", f.Name)
		}
		mode := f.Mode()
		if mode&os.ModeSymlink != 0 || (!mode.IsRegular() && !mode.IsDir()) {
			return "", fmt.Errorf("the bundle has a link or special file: %s", f.Name)
		}
		target := filepath.Join(tmp, filepath.FromSlash(name))
		if mode.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
			continue
		}
		total += int64(f.UncompressedSize64)
		if total > bundleMaxBytes {
			return "", errors.New("the bundle is too big")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		perm := os.FileMode(0o644)
		if mode&0o111 != 0 {
			perm = 0o755
		}
		if err := extractOne(f, target, perm); err != nil {
			return "", err
		}
	}
	_ = os.RemoveAll(dest)
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func extractOne(f *zip.File, target string, perm os.FileMode) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(rc, bundleMaxBytes)); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

var bundleRef = regexp.MustCompile(`\$\{([^}]*)\}`)

// Entry is EntryNamed without the name.
func (b *Bundle) Entry(dir string, values map[string]string) (map[string]any, error) {
	_, e, err := b.EntryNamed(dir, values)
	return e, err
}

// EntryNamed builds the mcp.json entry for a bundle installed in dir:
// ${__dirname} is dir, ${user_config.k} the value typed (or the
// default; a sensitive one is ${secret:<name>_<k>}), ${HOME} is
// ${userHome}. A required value that is missing is an error.
func (b *Bundle) EntryNamed(dir string, values map[string]string) (string, map[string]any, error) {
	var ferr error
	sub := func(s string) string {
		return bundleRef.ReplaceAllStringFunc(s, func(m string) string {
			ref := m[2 : len(m)-1]
			switch {
			case ref == "__dirname":
				return filepath.ToSlash(dir)
			case ref == "HOME":
				return "${userHome}"
			case ref == "pathSeparator" || ref == "/":
				return "/"
			case strings.HasPrefix(ref, "user_config."):
				k := strings.TrimPrefix(ref, "user_config.")
				f, ok := b.Manifest.UserConfig[k]
				if !ok {
					return ""
				}
				if f.Sensitive {
					return "${secret:" + b.SecretName(k) + "}"
				}
				v := values[k]
				if v == "" {
					v = f.DefaultText()
				}
				if v == "" && f.Required && ferr == nil {
					ferr = fmt.Errorf("%s is required", f.Title)
				}
				return strings.ReplaceAll(v, "${HOME}", "${userHome}")
			}
			return m
		})
	}
	c := b.Manifest.Server.MCPConfig
	args := make([]string, 0, len(c.Args))
	for _, a := range c.Args {
		args = append(args, sub(a))
	}
	entry := map[string]any{"command": sub(c.Command), "args": args,
		"bundle": map[string]any{"name": b.Manifest.Name, "version": b.Manifest.Version}}
	if len(c.Env) > 0 {
		env := map[string]any{}
		for k, v := range c.Env {
			env[k] = sub(v)
		}
		entry["env"] = env
	}
	if ferr != nil {
		return "", nil, ferr
	}
	return b.ServerName(), entry, nil
}
