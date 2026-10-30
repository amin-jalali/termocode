package containers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// ── Remote tags over the Registry HTTP API v2 (CP-12) ────────────────────
//
// `GET /v2/<name>/tags/list`, page by page through the `Link` header,
// with the registry's `WWW-Authenticate: Bearer realm=…,service=…,scope=…`
// token exchange.
//
// Security rules:
//   - Anonymous tokens only. Nothing here reads the engine's credential
//     store, a credential helper or the auth blobs of config.json, and the
//     token request carries no credentials. A private repository answers
//     ErrNeedsSignIn ("needs sign-in; pull by name still works": the
//     engine's own `pull` uses the user's login).
//   - HTTPS only. Plain http is tried only for a loopback registry
//     (localhost:5000, 127.0.0.1, [::1]) and only after its https answer
//     was "an HTTP response to an HTTPS client" (a local `registry:2`).
//   - A redirect to another scheme is never followed, nor a next page or a
//     token realm on plain http (outside loopback).
//   - Strict timeouts (connect and whole call), a size cap on every
//     answer, a cap on tags and pages, strict repository names, and only
//     tags that match the engine's tag grammar are kept.
//   - The token lives only inside one Tags call: it is never returned,
//     logged or put into an error.

// RemoteRepo is a repository on a registry, ready for the Registry HTTP
// API v2.
type RemoteRepo struct {
	Host    string // the registry as users write it ("docker.io", "ghcr.io", "localhost:5000")
	APIHost string // the host the API answers on ("registry-1.docker.io" for docker.io)
	Name    string // the repository path ("library/nginx", "acme/api")
}

// TagList is one answer of RegistryAPI.Tags.
type TagList struct {
	Repo      RemoteRepo
	Tags      []string
	Truncated bool // the tag or page cap ended the list
}

// ErrNeedsSignIn: the registry wants a login for this repository (or
// does not say whether it exists, like Docker Hub).
var ErrNeedsSignIn = errors.New("needs sign-in; pull by name still works")

// ErrRepoNotFound: the registry does not know the repository.
var ErrRepoNotFound = errors.New("repository not found")

// Defaults of RegistryAPI.
const (
	DefaultRegistryConnectTimeout = 5 * time.Second
	DefaultRegistryTimeout        = 20 * time.Second
	DefaultRegistryMaxTags        = 500
	DefaultRegistryMaxBody        = 1 << 20
	DefaultRegistryMaxPages       = 20
	registryPageSize              = 100
	registryMaxTokenBody          = 64 << 10
)

// RegistryAPI lists remote tags. The zero value uses the defaults.
type RegistryAPI struct {
	ConnectTimeout time.Duration  // dial and TLS handshake (0 = 5 s)
	Timeout        time.Duration  // the whole call, every page (0 = 20 s)
	MaxTags        int            // 0 = 500
	MaxBody        int64          // per answer (0 = 1 MiB)
	MaxPages       int            // 0 = 20
	RootCAs        *x509.CertPool // nil = the system roots (tests set it)
}

var (
	registryHostRe = lazyre.New(`^(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*|\[[0-9a-f:.]+\])(?::([0-9]{1,5}))?$`)
	repoNameRe     = lazyre.New(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*)*$`)
	tagRe          = lazyre.New(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
	digestRe       = lazyre.New(`^sha256:[a-f0-9]{64}$`)
)

// ParseRemoteRepo reads a reference like "nginx", "nginx:1.27",
// "bitnami/redis" or "ghcr.io/acme/api:1.4" (a tag or digest is
// dropped). It refuses anything that is not a plain image reference: a
// scheme, a query, a user, "..", upper-case path parts, control bytes.
func ParseRemoteRepo(ref string) (RemoteRepo, error) {
	bad := func(why string) (RemoteRepo, error) {
		return RemoteRepo{}, fmt.Errorf("%q is not an image repository: %s", clip(ref), why)
	}
	if ref == "" {
		return RemoteRepo{}, errors.New("give a repository like nginx or ghcr.io/acme/api")
	}
	if len(ref) > 512 {
		return bad("too long")
	}
	if strings.HasPrefix(ref, "-") {
		return bad("starts with a dash")
	}
	for _, r := range ref {
		if r <= ' ' || r >= 0x7f {
			return bad("has a space or a control character")
		}
	}
	if strings.Contains(ref, "://") || strings.ContainsAny(ref, "?#%\\") {
		return bad("only host/name[:tag] is accepted")
	}
	name := ref
	if i := strings.IndexByte(name, '@'); i >= 0 {
		if !digestRe.MatchString(name[i+1:]) {
			return bad("bad digest")
		}
		name = name[:i]
	}
	host := "docker.io"
	if first, rest, ok := strings.Cut(name, "/"); ok && (strings.ContainsAny(first, ".:[") || first == "localhost") {
		host, name = strings.ToLower(first), rest
	}
	if slash, colon := strings.LastIndexByte(name, '/'), strings.LastIndexByte(name, ':'); colon > slash {
		if !tagRe.MatchString(name[colon+1:]) {
			return bad("bad tag")
		}
		name = name[:colon]
	}
	m := registryHostRe.FindStringSubmatch(host)
	if m == nil {
		return bad("bad registry host")
	}
	if m[1] != "" {
		if p, err := strconv.Atoi(m[1]); err != nil || p < 1 || p > 65535 {
			return bad("bad port")
		}
	}
	if strings.Contains(name, "..") || len(name) > 255 || !repoNameRe.MatchString(name) {
		return bad("the name must be lower-case parts like acme/api")
	}
	api := host
	if dockerHubHosts[host] {
		host, api = "docker.io", "registry-1.docker.io"
		if !strings.Contains(name, "/") {
			name = "library/" + name
		}
	}
	return RemoteRepo{Host: host, APIHost: api, Name: name}, nil
}

// Ref is the reference users pull by: "nginx", "bitnami/redis",
// "ghcr.io/acme/api".
func (r RemoteRepo) Ref() string {
	if r.Host == "docker.io" {
		return strings.TrimPrefix(r.Name, "library/")
	}
	return r.Host + "/" + r.Name
}

// plainHTTPAllowed: a loopback registry (localhost, 127.0.0.0/8, ::1)
// may answer on plain http; nothing else may.
func plainHTTPAllowed(host string) bool {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	h = strings.Trim(strings.ToLower(h), "[]")
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (a *RegistryAPI) connectTimeout() time.Duration {
	if a.ConnectTimeout > 0 {
		return a.ConnectTimeout
	}
	return DefaultRegistryConnectTimeout
}

func (a *RegistryAPI) timeout() time.Duration {
	if a.Timeout > 0 {
		return a.Timeout
	}
	return DefaultRegistryTimeout
}

func (a *RegistryAPI) maxTags() int {
	if a.MaxTags > 0 {
		return a.MaxTags
	}
	return DefaultRegistryMaxTags
}

func (a *RegistryAPI) maxBody() int64 {
	if a.MaxBody > 0 {
		return a.MaxBody
	}
	return DefaultRegistryMaxBody
}

func (a *RegistryAPI) maxPages() int {
	if a.MaxPages > 0 {
		return a.MaxPages
	}
	return DefaultRegistryMaxPages
}

func (a *RegistryAPI) transport() *http.Transport {
	d := &net.Dialer{Timeout: a.connectTimeout(), KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy:                  http.ProxyFromEnvironment,
		DialContext:            d.DialContext,
		TLSClientConfig:        &tls.Config{RootCAs: a.RootCAs, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    a.connectTimeout(),
		ResponseHeaderTimeout:  a.timeout(),
		MaxResponseHeaderBytes: 64 << 10,
		ForceAttemptHTTP2:      true,
	}
}

// errOtherScheme ends a redirect to another scheme.
var errOtherScheme = errors.New("redirect to another scheme refused")

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("too many redirects")
	}
	if req.URL.Scheme != via[0].URL.Scheme {
		return errOtherScheme
	}
	return nil
}

// tagSession is one Tags call: its client and (after the exchange) its
// token. It never leaves this file.
type tagSession struct {
	api    *RegistryAPI
	client *http.Client
	repo   RemoteRepo
	token  string
	asked  bool // the token exchange ran
}

// Tags lists the repository's tags (in the registry's order).
func (a *RegistryAPI) Tags(ctx context.Context, repo RemoteRepo) (TagList, error) {
	if err := repo.valid(); err != nil {
		return TagList{Repo: repo}, err
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	tr := a.transport()
	defer tr.CloseIdleConnections()
	s := &tagSession{api: a, client: &http.Client{Transport: tr, CheckRedirect: checkRedirect}, repo: repo}
	first := "/v2/" + repo.Name + "/tags/list?n=" + strconv.Itoa(registryPageSize)
	next := "https://" + repo.APIHost + first
	out := TagList{Repo: repo}
	limit := a.maxTags()
	for page := 0; next != ""; page++ {
		if page >= a.maxPages() {
			out.Truncated = true
			break
		}
		body, link, err := s.get(ctx, next)
		if err != nil && page == 0 && plainHTTPAllowed(repo.APIHost) && isHTTPToHTTPS(err) {
			next = "http://" + repo.APIHost + first
			body, link, err = s.get(ctx, next)
		}
		if err != nil {
			return TagList{Repo: repo}, a.callError(ctx, err)
		}
		var answer struct {
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal(body, &answer); err != nil {
			return TagList{Repo: repo}, errors.New("the registry sent an answer that is not a tag list")
		}
		for i, tag := range answer.Tags {
			if !tagRe.MatchString(tag) {
				continue
			}
			if len(out.Tags) >= limit {
				out.Truncated = true
				return out, nil
			}
			out.Tags = append(out.Tags, tag)
			if len(out.Tags) == limit && (i < len(answer.Tags)-1 || link != "") {
				out.Truncated = true
				return out, nil
			}
		}
		if next, err = nextPage(next, link); err != nil {
			return TagList{Repo: repo}, err
		}
	}
	return out, nil
}

func (r RemoteRepo) valid() error {
	if r.APIHost == "" || registryHostRe.FindStringSubmatch(r.APIHost) == nil {
		return errors.New("bad registry host")
	}
	if strings.Contains(r.Name, "..") || len(r.Name) > 255 || !repoNameRe.MatchString(r.Name) {
		return errors.New("bad repository name")
	}
	return nil
}

// isHTTPToHTTPS: the server answered an https request in plain http.
func isHTTPToHTTPS(err error) bool {
	return strings.Contains(err.Error(), "server gave HTTP response to HTTPS client")
}

// callError says why a call failed in fixed words plus the transport's
// cause (never a header, so never the token).
func (a *RegistryAPI) callError(ctx context.Context, err error) error {
	if errors.Is(err, ErrNeedsSignIn) || errors.Is(err, ErrRepoNotFound) {
		return err
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return fmt.Errorf("the registry timed out after %s", a.timeout())
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		if errors.Is(ue.Err, errOtherScheme) {
			return fmt.Errorf("the registry's %v", errOtherScheme)
		}
		return fmt.Errorf("cannot reach the registry: %v", ue.Err)
	}
	return err
}

// get fetches one page; a 401 runs the anonymous token exchange once.
func (s *tagSession) get(ctx context.Context, u string) ([]byte, string, error) {
	resp, err := s.do(ctx, u, s.token)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode == http.StatusUnauthorized && !s.asked {
		challenge := resp.Header.Get("WWW-Authenticate")
		drain(resp)
		s.asked = true
		scheme, params := parseChallenge(challenge)
		if scheme != "bearer" {
			return nil, "", ErrNeedsSignIn
		}
		tok, err := s.fetchToken(ctx, params)
		if err != nil {
			return nil, "", err
		}
		s.token = tok
		if resp, err = s.do(ctx, u, s.token); err != nil {
			return nil, "", err
		}
	}
	defer drain(resp)
	switch code := resp.StatusCode; {
	case code == http.StatusOK:
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return nil, "", ErrNeedsSignIn
	case code == http.StatusNotFound:
		return nil, "", ErrRepoNotFound
	case code == http.StatusTooManyRequests:
		return nil, "", errors.New("the registry says too many requests; try again later")
	default:
		return nil, "", fmt.Errorf("the registry answered %d", code)
	}
	body, err := readCapped(resp.Body, s.api.maxBody())
	if err != nil {
		return nil, "", err
	}
	return body, resp.Header.Get("Link"), nil
}

func (s *tagSession) do(ctx context.Context, u, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return s.client.Do(req)
}

// fetchToken asks the realm for an anonymous pull token.
func (s *tagSession) fetchToken(ctx context.Context, params map[string]string) (string, error) {
	realm := params["realm"]
	if realm == "" {
		return "", ErrNeedsSignIn
	}
	u, err := url.Parse(realm)
	if err != nil || !u.IsAbs() || u.Host == "" || u.User != nil {
		return "", errors.New("the registry named a token server that is not a plain https URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && plainHTTPAllowed(u.Host)) {
		return "", errors.New("the registry's token server must use https")
	}
	q := u.Query()
	if v := params["service"]; v != "" {
		q.Set("service", v)
	}
	scope := params["scope"]
	if scope == "" {
		scope = "repository:" + s.repo.Name + ":pull"
	}
	q.Set("scope", scope)
	u.RawQuery = q.Encode()
	resp, err := s.do(ctx, u.String(), "")
	if err != nil {
		return "", err
	}
	defer drain(resp)
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", ErrNeedsSignIn
	default:
		return "", fmt.Errorf("the token server answered %d", resp.StatusCode)
	}
	body, err := readCapped(resp.Body, registryMaxTokenBody)
	if err != nil {
		return "", err
	}
	var t struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(body, &t) != nil {
		return "", errors.New("the token server sent no usable token")
	}
	tok := t.Token
	if tok == "" {
		tok = t.AccessToken
	}
	if tok == "" || len(tok) > 16<<10 {
		return "", errors.New("the token server sent no usable token")
	}
	for _, r := range tok {
		if r <= ' ' || r >= 0x7f {
			return "", errors.New("the token server sent no usable token")
		}
	}
	return tok, nil
}

func readCapped(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("the registry answer is too large (over %d bytes)", max)
	}
	return b, nil
}

// drain reads a little of an unused body so the connection can be reused,
// then closes it.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()
}

// parseChallenge reads `Bearer realm="…",service="…",scope="…"`: the
// lower-case scheme and its parameters.
func parseChallenge(h string) (string, map[string]string) {
	h = strings.TrimSpace(h)
	scheme, rest, _ := strings.Cut(h, " ")
	params := map[string]string{}
	for rest = strings.TrimSpace(rest); rest != ""; {
		key, after, ok := strings.Cut(rest, "=")
		if !ok {
			break
		}
		key = strings.ToLower(strings.TrimSpace(key))
		after = strings.TrimSpace(after)
		var val string
		if strings.HasPrefix(after, `"`) {
			end := strings.IndexByte(after[1:], '"')
			if end < 0 {
				break
			}
			val, after = after[1:1+end], after[2+end:]
		} else {
			val, after, _ = strings.Cut(after, ",")
			after = "," + after
		}
		params[key] = val
		after = strings.TrimSpace(after)
		rest = strings.TrimSpace(strings.TrimPrefix(after, ","))
	}
	return strings.ToLower(scheme), params
}

// nextPage reads `<…>; rel="next"` from a Link header ("" = no more). The
// next page must stay on the same scheme and host.
func nextPage(current, link string) (string, error) {
	if link == "" {
		return "", nil
	}
	for _, part := range strings.Split(link, ",") {
		target, attrs, ok := strings.Cut(strings.TrimSpace(part), ";")
		if !ok || !strings.Contains(strings.ReplaceAll(strings.ToLower(attrs), " ", ""), `rel="next"`) && !strings.Contains(strings.ToLower(attrs), "rel=next") {
			continue
		}
		target = strings.TrimSpace(target)
		if !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
			return "", errors.New("the registry sent a bad next-page link")
		}
		cur, err := url.Parse(current)
		if err != nil {
			return "", err
		}
		next, err := cur.Parse(target[1 : len(target)-1])
		if err != nil {
			return "", errors.New("the registry sent a bad next-page link")
		}
		if next.Scheme != cur.Scheme || next.Host != cur.Host || next.User != nil || !strings.HasPrefix(next.Path, "/v2/") {
			return "", errors.New("the registry sent the next page on another host; refused")
		}
		return next.String(), nil
	}
	return "", nil
}
