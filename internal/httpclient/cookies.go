package httpclient

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Cookie is one stored cookie (API-11). Expires zero = session cookie
// (kept until deleted: an API client keeps its login between runs).
type Cookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires,omitzero"`
	Secure   bool      `json:"secure,omitempty"`
	HTTPOnly bool      `json:"http_only,omitempty"`
	HostOnly bool      `json:"host_only,omitempty"`
}

// expired reports whether c is past its expiry at now.
func (c Cookie) expired(now time.Time) bool {
	return !c.Expires.IsZero() && !c.Expires.After(now)
}

// CookieStore keeps one cookie jar per environment ("" = no
// environment) and, with a Path, saves every change to that file
// (folder 0700, file 0600). It is safe for concurrent use.
type CookieStore struct {
	mu   sync.Mutex
	path string
	envs map[string][]Cookie
	now  func() time.Time
}

// NewCookieStore makes an empty store; path "" keeps it in memory.
func NewCookieStore(path string) *CookieStore {
	return &CookieStore{path: path, envs: map[string][]Cookie{}, now: time.Now}
}

// LoadCookieStore reads the store at path. A missing file is an empty
// store; a broken file gives an empty store and the error (the next
// change writes a good file).
func LoadCookieStore(path string) (*CookieStore, error) {
	s := NewCookieStore(path)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var envs map[string][]Cookie
	if err := json.Unmarshal(b, &envs); err != nil {
		return s, err
	}
	now := s.now()
	for env, list := range envs {
		for _, c := range list {
			if c.Name != "" && c.Domain != "" && !c.expired(now) {
				s.envs[env] = append(s.envs[env], c)
			}
		}
	}
	return s, nil
}

// Envs lists the environments that have cookies, sorted.
func (s *CookieStore) Envs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for env, list := range s.envs {
		if len(list) > 0 {
			out = append(out, env)
		}
	}
	sort.Strings(out)
	return out
}

// List returns the live cookies of env, sorted by name, domain, path.
func (s *CookieStore) List(env string) []Cookie {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropExpired(env)
	out := append([]Cookie(nil), s.envs[env]...)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Domain != b.Domain {
			return a.Domain < b.Domain
		}
		return a.Path < b.Path
	})
	return out
}

// Set adds c to env or replaces the cookie with the same domain, path
// and name, then saves.
func (s *CookieStore) Set(env string, c Cookie) error {
	if c.Path == "" {
		c.Path = "/"
	}
	c.Domain = strings.ToLower(strings.TrimPrefix(c.Domain, "."))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.put(env, c)
	return s.saveLocked()
}

// Delete removes one cookie of env; false = not found.
func (s *CookieStore) Delete(env, domain, path, name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.remove(env, domain, path, name) {
		return false
	}
	_ = s.saveLocked()
	return true
}

// Clear removes every cookie of env and saves.
func (s *CookieStore) Clear(env string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.envs, env)
	return s.saveLocked()
}

// Jar is the http.CookieJar of env.
func (s *CookieStore) Jar(env string) http.CookieJar { return envJar{s: s, env: env} }

func (s *CookieStore) put(env string, c Cookie) {
	list := s.envs[env]
	for i := range list {
		if list[i].Domain == c.Domain && list[i].Path == c.Path && list[i].Name == c.Name {
			list[i] = c
			return
		}
	}
	s.envs[env] = append(list, c)
}

func (s *CookieStore) remove(env, domain, path, name string) bool {
	list := s.envs[env]
	for i := range list {
		if list[i].Domain == domain && list[i].Path == path && list[i].Name == name {
			s.envs[env] = append(list[:i:i], list[i+1:]...)
			if len(s.envs[env]) == 0 {
				delete(s.envs, env)
			}
			return true
		}
	}
	return false
}

func (s *CookieStore) dropExpired(env string) {
	now := s.now()
	list := s.envs[env]
	keep := list[:0]
	for _, c := range list {
		if !c.expired(now) {
			keep = append(keep, c)
		}
	}
	if len(keep) == 0 {
		delete(s.envs, env)
		return
	}
	s.envs[env] = keep
}

func (s *CookieStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.envs, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(s.path, append(b, '\n'))
}

// envJar is the http.CookieJar view of one environment (RFC 6265 rules
// for domain, path, secure and expiry; no public-suffix list, so a
// cookie for a bare top-level name like "com" is refused).
type envJar struct {
	s   *CookieStore
	env string
}

func (j envJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	host := canonicalHost(u.Host)
	if host == "" {
		return
	}
	s := j.s
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	changed := false
	for _, hc := range cookies {
		if hc == nil || hc.Name == "" {
			continue
		}
		c := Cookie{Name: hc.Name, Value: hc.Value, Secure: hc.Secure, HTTPOnly: hc.HttpOnly}
		domain := strings.ToLower(strings.TrimPrefix(hc.Domain, "."))
		switch {
		case domain == "" || domain == host:
			c.Domain, c.HostOnly = host, domain == ""
		case net.ParseIP(host) == nil && strings.HasSuffix(host, "."+domain) && strings.Contains(domain, "."):
			c.Domain = domain
		default:
			continue // another site's domain: refused
		}
		c.Path = hc.Path
		if c.Path == "" || c.Path[0] != '/' {
			c.Path = defaultCookiePath(u.Path)
		}
		switch {
		case hc.MaxAge < 0:
			c.Expires = now.Add(-time.Second)
		case hc.MaxAge > 0:
			c.Expires = now.Add(time.Duration(hc.MaxAge) * time.Second)
		case !hc.Expires.IsZero():
			c.Expires = hc.Expires
		}
		if c.expired(now) {
			changed = s.remove(j.env, c.Domain, c.Path, c.Name) || changed
			continue
		}
		s.put(j.env, c)
		changed = true
	}
	if changed {
		_ = s.saveLocked()
	}
}

func (j envJar) Cookies(u *url.URL) []*http.Cookie {
	host := canonicalHost(u.Host)
	if host == "" {
		return nil
	}
	secure := u.Scheme == "https" || u.Scheme == "wss"
	p := u.Path
	if p == "" {
		p = "/"
	}
	s := j.s
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropExpired(j.env)
	var match []Cookie
	for _, c := range s.envs[j.env] {
		if c.Secure && !secure {
			continue
		}
		if c.HostOnly && host != c.Domain || !c.HostOnly && host != c.Domain && !strings.HasSuffix(host, "."+c.Domain) {
			continue
		}
		if !cookiePathMatch(p, c.Path) {
			continue
		}
		match = append(match, c)
	}
	// Longer paths first (RFC 6265 5.4).
	sort.SliceStable(match, func(a, b int) bool { return len(match[a].Path) > len(match[b].Path) })
	out := make([]*http.Cookie, 0, len(match))
	for _, c := range match {
		out = append(out, &http.Cookie{Name: c.Name, Value: c.Value})
	}
	return out
}

func canonicalHost(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.ToLower(strings.TrimSuffix(strings.Trim(h, "[]"), "."))
}

// defaultCookiePath is the folder of the request path (RFC 6265 5.1.4).
func defaultCookiePath(p string) string {
	i := strings.LastIndex(p, "/")
	if p == "" || p[0] != '/' || i <= 0 {
		return "/"
	}
	return p[:i]
}

func cookiePathMatch(reqPath, cookiePath string) bool {
	if reqPath == cookiePath {
		return true
	}
	if !strings.HasPrefix(reqPath, cookiePath) {
		return false
	}
	return strings.HasSuffix(cookiePath, "/") || reqPath[len(cookiePath)] == '/'
}
