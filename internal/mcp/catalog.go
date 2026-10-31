package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/cfgfile"
)

// Catalog (MCPX-16): browse servers from the official MCP Registry or a
// custom catalog URL with the same API (mcp_catalog_url: an org's
// approved list). The network is used only when the user asks (Fetch);
// the last list is kept on disk (Cached) for the next open.

// OfficialRegistryURL is the default catalog.
const OfficialRegistryURL = "https://registry.modelcontextprotocol.io"

// catalogMaxPages caps one fetch (100 servers per page).
const catalogMaxPages = 20

// catalogMaxPage caps one page's body.
const catalogMaxPage = 8 << 20

// Catalog reads one registry.
type Catalog struct {
	URL      string // "" = OfficialRegistryURL
	CacheDir string // "" = no cache
	HTTP     *http.Client
}

// CatalogEntry is one server of the catalog.
type CatalogEntry struct {
	Name        string           `json:"name"` // reverse-DNS: io.github.acme/notes
	Title       string           `json:"title,omitempty"`
	Description string           `json:"description,omitempty"`
	Version     string           `json:"version,omitempty"`
	Repo        string           `json:"repo,omitempty"`
	Publisher   string           `json:"publisher,omitempty"` // github.com/acme, example.com
	Verified    bool             `json:"verified,omitempty"`  // namespace checked by the official registry
	Status      string           `json:"status,omitempty"`    // active, deprecated, deleted
	UpdatedAt   string           `json:"updatedAt,omitempty"`
	Source      string           `json:"source,omitempty"` // the catalog's base URL
	Packages    []CatalogPackage `json:"packages,omitempty"`
	Remotes     []CatalogRemote  `json:"remotes,omitempty"`
}

// CatalogPackage is a way to run the server locally.
type CatalogPackage struct {
	RegistryType string       `json:"registryType"` // npm, pypi, oci
	Identifier   string       `json:"identifier"`
	Version      string       `json:"version,omitempty"`
	Transport    string       `json:"transport,omitempty"`
	Env          []CatalogVar `json:"env,omitempty"`
}

// CatalogRemote is a hosted endpoint.
type CatalogRemote struct {
	Type    string       `json:"type"` // streamable-http, sse
	URL     string       `json:"url"`
	Headers []CatalogVar `json:"headers,omitempty"`
}

// CatalogVar is an environment variable or a header the server needs.
type CatalogVar struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     string `json:"default,omitempty"`
}

// Deprecated reports a server its publisher marked deprecated or removed.
func (e CatalogEntry) Deprecated() bool { return e.Status == "deprecated" || e.Status == "deleted" }

func (c Catalog) base() string {
	u := strings.TrimRight(strings.TrimSpace(c.URL), "/")
	if u == "" {
		return OfficialRegistryURL
	}
	return u
}

func (c Catalog) official() bool { return c.base() == OfficialRegistryURL }

// Host is the catalog's host, for the picker title.
func (c Catalog) Host() string {
	if u, err := url.Parse(c.base()); err == nil && u.Host != "" {
		return u.Host
	}
	return c.base()
}

// CheckCatalogURL accepts https, or http on a loopback host.
func CheckCatalogURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("catalog URL %q is not a URL", raw)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" {
		h := u.Hostname()
		if ip := net.ParseIP(h); h == "localhost" || (ip != nil && ip.IsLoopback()) {
			return nil
		}
	}
	return fmt.Errorf("catalog URL must use https: %s", raw)
}

type wireServer struct {
	Server struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Version     string `json:"version"`
		Repository  struct {
			URL string `json:"url"`
		} `json:"repository"`
		Packages []struct {
			RegistryType string `json:"registryType"`
			Identifier   string `json:"identifier"`
			Version      string `json:"version"`
			Transport    struct {
				Type string `json:"type"`
			} `json:"transport"`
			Env []wireVar `json:"environmentVariables"`
		} `json:"packages"`
		Remotes []struct {
			Type    string    `json:"type"`
			URL     string    `json:"url"`
			Headers []wireVar `json:"headers"`
		} `json:"remotes"`
	} `json:"server"`
	Meta map[string]struct {
		Status    string `json:"status"`
		UpdatedAt string `json:"updatedAt"`
	} `json:"_meta"`
}

type wireVar struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsSecret    bool   `json:"isSecret"`
	IsRequired  bool   `json:"isRequired"`
	Default     string `json:"default"`
}

func (v wireVar) entry() CatalogVar {
	return CatalogVar{Name: v.Name, Description: v.Description, Secret: v.IsSecret, Required: v.IsRequired, Default: v.Default}
}

// Fetch reads the whole catalog (paged) and caches it. A failed fetch
// keeps the old cache.
func (c Catalog) Fetch(ctx context.Context) ([]CatalogEntry, error) {
	out, err := c.fetchPages(ctx, nil)
	if err != nil {
		return nil, err
	}
	c.save(out)
	return out, nil
}

// FetchSince reads only the servers changed since t (the registry's
// updated_since) and merges them into the cache; it returns the changed
// ones.
func (c Catalog) FetchSince(ctx context.Context, t time.Time) ([]CatalogEntry, error) {
	changed, err := c.fetchPages(ctx, url.Values{"updated_since": {t.UTC().Format(time.RFC3339)}})
	if err != nil {
		return nil, err
	}
	old, _, _ := c.Cached()
	c.save(latestOnly(append(old, changed...)))
	return changed, nil
}

func (c Catalog) fetchPages(ctx context.Context, extra url.Values) ([]CatalogEntry, error) {
	base := c.base()
	if err := CheckCatalogURL(base); err != nil {
		return nil, err
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	var out []CatalogEntry
	cursor := ""
	for page := 0; page < catalogMaxPages; page++ {
		q := url.Values{"limit": {"100"}}
		for k, v := range extra {
			q[k] = v
		}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v0/servers?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := hc.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, catalogMaxPage))
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("catalog %s: %s", c.Host(), resp.Status)
		}
		var w struct {
			Servers  []wireServer `json:"servers"`
			Metadata struct {
				NextCursor string `json:"nextCursor"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(body, &w); err != nil {
			return nil, fmt.Errorf("catalog %s: not a server list: %w", c.Host(), err)
		}
		for _, s := range w.Servers {
			out = append(out, c.entry(s))
		}
		if w.Metadata.NextCursor == "" || w.Metadata.NextCursor == cursor {
			break
		}
		cursor = w.Metadata.NextCursor
	}
	return latestOnly(out), nil
}

func (c Catalog) entry(s wireServer) CatalogEntry {
	v := s.Server
	e := CatalogEntry{Name: v.Name, Title: v.Title, Description: v.Description, Version: v.Version,
		Repo: v.Repository.URL, Publisher: publisherOf(v.Name), Verified: c.official(), Source: c.base()}
	for _, m := range s.Meta {
		if m.Status != "" {
			e.Status = m.Status
		}
		if m.UpdatedAt != "" {
			e.UpdatedAt = m.UpdatedAt
		}
	}
	for _, p := range v.Packages {
		cp := CatalogPackage{RegistryType: p.RegistryType, Identifier: p.Identifier, Version: p.Version, Transport: p.Transport.Type}
		for _, x := range p.Env {
			cp.Env = append(cp.Env, x.entry())
		}
		e.Packages = append(e.Packages, cp)
	}
	for _, r := range v.Remotes {
		cr := CatalogRemote{Type: r.Type, URL: r.URL}
		for _, x := range r.Headers {
			cr.Headers = append(cr.Headers, x.entry())
		}
		e.Remotes = append(e.Remotes, cr)
	}
	return e
}

// latestOnly keeps one entry per name (the last listed version wins:
// the registry lists versions in order).
func latestOnly(in []CatalogEntry) []CatalogEntry {
	idx := map[string]int{}
	var out []CatalogEntry
	for _, e := range in {
		if i, ok := idx[e.Name]; ok {
			out[i] = e
			continue
		}
		idx[e.Name] = len(out)
		out = append(out, e)
	}
	return out
}

// publisherOf turns the reverse-DNS namespace into a readable one:
// io.github.acme/notes → github.com/acme; com.example/x → example.com.
func publisherOf(name string) string {
	ns, _, _ := strings.Cut(name, "/")
	parts := strings.Split(ns, ".")
	if len(parts) >= 3 && parts[0] == "io" && parts[1] == "github" {
		return "github.com/" + strings.Join(parts[2:], ".")
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, ".")
}

type catalogCache struct {
	URL       string         `json:"url"`
	FetchedAt time.Time      `json:"fetchedAt"`
	Entries   []CatalogEntry `json:"entries"`
}

func (c Catalog) cachePath() string {
	if c.CacheDir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(c.base()))
	return filepath.Join(c.CacheDir, "catalog-"+hex.EncodeToString(sum[:])[:12]+".json")
}

func (c Catalog) save(list []CatalogEntry) {
	p := c.cachePath()
	if p == "" {
		return
	}
	b, err := json.Marshal(catalogCache{URL: c.base(), FetchedAt: time.Now().UTC(), Entries: list})
	if err == nil {
		_ = cfgfile.WriteFile(p, b)
	}
}

// Cached returns the last fetched list (never the network).
func (c Catalog) Cached() ([]CatalogEntry, time.Time, bool) {
	p := c.cachePath()
	if p == "" {
		return nil, time.Time{}, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, time.Time{}, false
	}
	var cc catalogCache
	if json.Unmarshal(b, &cc) != nil || cc.URL != c.base() {
		return nil, time.Time{}, false
	}
	return cc.Entries, cc.FetchedAt, true
}

// ShortName is the mcp.json name: the part after "/", safe characters.
func (e CatalogEntry) ShortName() string {
	_, n, ok := strings.Cut(e.Name, "/")
	if !ok {
		n = e.Name
	}
	n = strings.ToLower(n)
	n = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, n)
	return strings.Trim(n, "-")
}

func secretRef(server, v string) string {
	return strings.ToLower(strings.ReplaceAll(server+"_"+v, "-", "_"))
}

// Secrets lists the ${secret:} names the entry from ServerEntry needs.
func (e CatalogEntry) Secrets() []string {
	_, entry, err := e.ServerEntry()
	if err != nil {
		return nil
	}
	b, _ := json.Marshal(entry)
	seen := map[string]bool{}
	var out []string
	for _, m := range refRe.FindAllStringSubmatch(string(b), -1) {
		if n, ok := strings.CutPrefix(m[1], "secret:"); ok && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// registryKey is the entry's "registry" key: where it came from, so the
// version check can find it again.
func (e CatalogEntry) registryKey() map[string]any {
	k := map[string]any{"name": e.Name, "version": e.Version}
	if e.Source != "" && e.Source != OfficialRegistryURL {
		k["url"] = e.Source
	}
	return k
}

// ServerEntry turns the entry into an mcp.json server: the first
// remote, else the first npm / pypi / oci package, pinned to its
// version. Secrets become ${secret:<name>_<var>}; other variables get
// their default or ${VAR} from the environment.
func (e CatalogEntry) ServerEntry() (string, map[string]any, error) {
	name := e.ShortName()
	if name == "" {
		return "", nil, errors.New("the catalog entry has no name")
	}
	vars := func(vs []CatalogVar) map[string]any {
		out := map[string]any{}
		for _, v := range vs {
			switch {
			case v.Secret:
				out[v.Name] = "${secret:" + secretRef(name, v.Name) + "}"
			case v.Default != "":
				out[v.Name] = v.Default
			default:
				out[v.Name] = "${" + v.Name + "}"
			}
		}
		return out
	}
	for _, r := range e.Remotes {
		if err := CheckCatalogURL(r.URL); err != nil || strings.HasPrefix(r.URL, "http:") {
			continue
		}
		if r.Type == "sse" {
			continue // not supported (parseEntry)
		}
		entry := map[string]any{"type": TypeHTTP, "url": r.URL, "registry": e.registryKey()}
		if len(r.Headers) > 0 {
			entry["headers"] = vars(r.Headers) // a secret header holds the whole value (Bearer …)
		}
		return name, entry, nil
	}
	for _, p := range e.Packages {
		if p.Transport != "" && p.Transport != "stdio" {
			continue
		}
		var cmd string
		var args []string
		switch p.RegistryType {
		case "npm":
			id := p.Identifier
			if p.Version != "" {
				id += "@" + p.Version
			}
			cmd, args = "npx", []string{"-y", id}
		case "pypi":
			id := p.Identifier
			if p.Version != "" {
				id += "==" + p.Version
			}
			cmd, args = "uvx", []string{id}
		case "oci":
			id := p.Identifier
			if p.Version != "" && !strings.Contains(id, ":") {
				id += ":" + p.Version
			}
			args = []string{"run", "-i", "--rm"}
			for _, v := range p.Env {
				args = append(args, "-e", v.Name) // the container gets the entry's env by name
			}
			cmd, args = "docker", append(args, id)
		default:
			continue
		}
		entry := map[string]any{"command": cmd, "args": args, "registry": e.registryKey()}
		if len(p.Env) > 0 {
			entry["env"] = vars(p.Env)
		}
		return name, entry, nil
	}
	return "", nil, fmt.Errorf("%s has no package or remote termocode can run (npm, pypi, oci, https)", e.Name)
}
