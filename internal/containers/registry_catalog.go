package containers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// ── Catalog browse over the Registry HTTP API v2 (CTX-35) ────────────────
//
// `GET /v2/_catalog`, page by page through the `Link` header, with the
// same rules as Tags (registryapi.go): anonymous tokens only (scope
// registry:catalog:*), HTTPS only (plain http only for a loopback
// registry), strict timeouts, size and page caps, and only names that
// match the repository grammar are kept.

// Catalog is one answer of RegistryAPI.Catalog.
type Catalog struct {
	Host      string
	Repos     []string
	Truncated bool // the repository or page cap ended the list
}

// ErrNoCatalog: the registry does not offer a catalog (Docker Hub).
var ErrNoCatalog = errors.New("this registry has no catalog; search by name instead")

// Catalog lists the repositories of the registry at host (as users write
// it: "ghcr.io", "localhost:5000"), in the registry's order. MaxTags caps
// the number of repositories too.
func (a *RegistryAPI) Catalog(ctx context.Context, host string) (Catalog, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	out := Catalog{Host: host}
	switch host {
	case "docker.io", "index.docker.io", "registry-1.docker.io":
		return out, ErrNoCatalog
	}
	if host == "" || registryHostRe.FindStringSubmatch(host) == nil {
		return out, errors.New("bad registry host")
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	tr := a.transport()
	defer tr.CloseIdleConnections()
	s := &tagSession{api: a, client: &http.Client{Transport: tr, CheckRedirect: checkRedirect}, repo: RemoteRepo{Host: host, APIHost: host}, scope: "registry:catalog:*"}
	first := "/v2/_catalog?n=" + strconv.Itoa(registryPageSize)
	next := "https://" + host + first
	limit := a.maxTags()
	for page := 0; next != ""; page++ {
		if page >= a.maxPages() {
			out.Truncated = true
			break
		}
		body, link, err := s.get(ctx, next)
		if err != nil && page == 0 && plainHTTPAllowed(host) && isHTTPToHTTPS(err) {
			next = "http://" + host + first
			body, link, err = s.get(ctx, next)
		}
		if err != nil {
			return Catalog{Host: host}, a.callError(ctx, err)
		}
		var answer struct {
			Repositories []string `json:"repositories"`
		}
		if err := json.Unmarshal(body, &answer); err != nil {
			return Catalog{Host: host}, errors.New("the registry sent an answer that is not a catalog")
		}
		for i, name := range answer.Repositories {
			if strings.Contains(name, "..") || len(name) > 255 || !repoNameRe.MatchString(name) {
				continue
			}
			out.Repos = append(out.Repos, name)
			if len(out.Repos) == limit {
				if i < len(answer.Repositories)-1 || link != "" {
					out.Truncated = true
				}
				return out, nil
			}
		}
		if next, err = nextPage(next, link); err != nil {
			return Catalog{Host: host}, err
		}
	}
	return out, nil
}
