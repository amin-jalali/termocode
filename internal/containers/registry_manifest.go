package containers

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// ── Manifest digest and delete over the Registry HTTP API v2 (CTX-35) ────
//
// ManifestDigest is `HEAD /v2/<name>/manifests/<tag>` (any manifest kind):
// the Docker-Content-Digest of the tag. DeleteManifest is `DELETE
// /v2/<name>/manifests/<digest>`: it takes a digest only (the API deletes
// by digest, so every tag on that manifest goes), and needs the user's
// Login. The same rules as Tags apply (registryapi.go).

// manifestAccept is every manifest kind, so the registry answers with the
// digest of what it stores (an index for a multi-arch tag).
const manifestAccept = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

func (a *RegistryAPI) manifestCall(ctx context.Context, repo RemoteRepo, method, ref, scope string) (http.Header, error) {
	if err := repo.valid(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	tr := a.transport()
	defer tr.CloseIdleConnections()
	s := &tagSession{api: a, client: &http.Client{Transport: tr, CheckRedirect: checkRedirect}, repo: repo,
		scope: scope, method: method, accept: manifestAccept}
	path := "/v2/" + repo.Name + "/manifests/" + ref
	_, _, err := s.get(ctx, "https://"+repo.APIHost+path)
	if err != nil && plainHTTPAllowed(repo.APIHost) && isHTTPToHTTPS(err) {
		_, _, err = s.get(ctx, "http://"+repo.APIHost+path)
	}
	if err != nil {
		return nil, a.callError(ctx, err)
	}
	return s.header, nil
}

// ManifestDigest is the registry's digest of repo:tag.
func (a *RegistryAPI) ManifestDigest(ctx context.Context, repo RemoteRepo, tag string) (string, error) {
	if !tagRe.MatchString(tag) {
		return "", errors.New("bad tag")
	}
	h, err := a.manifestCall(ctx, repo, http.MethodHead, tag, "")
	if err != nil {
		return "", err
	}
	d := strings.TrimSpace(h.Get("Docker-Content-Digest"))
	if !digestRe.MatchString(d) {
		return "", errors.New("the registry sent no usable digest")
	}
	return d, nil
}

// DeleteManifest deletes the manifest with digest (every tag on it).
func (a *RegistryAPI) DeleteManifest(ctx context.Context, repo RemoteRepo, digest string) error {
	if !digestRe.MatchString(digest) {
		return errors.New("delete takes a sha256 digest")
	}
	if a.Login == nil {
		return errors.New("deleting needs your login")
	}
	_, err := a.manifestCall(ctx, repo, http.MethodDelete, digest, "repository:"+repo.Name+":pull,delete")
	return err
}

// LoginHint is how to sign in to a cloud registry with its own CLI.
type LoginHint struct {
	Provider string
	Command  string // a shell line for the user's terminal ("" = none)
	Note     string
}

// CloudLoginHint names the cloud CLI login for host (ok=false: a plain
// registry). The commands are shown and copied, never run by termocode.
func CloudLoginHint(host string) (LoginHint, bool) {
	h := strings.ToLower(strings.TrimSpace(host))
	switch {
	case strings.Contains(h, ".dkr.ecr.") && strings.HasSuffix(h, ".amazonaws.com"):
		region := strings.TrimSuffix(h[strings.Index(h, ".dkr.ecr.")+len(".dkr.ecr."):], ".amazonaws.com")
		return LoginHint{Provider: "AWS ECR",
			Command: "aws ecr get-login-password --region " + region + " | docker login --username AWS --password-stdin " + h,
			Note:    "The token from the AWS CLI is good for 12 hours."}, true
	case strings.HasSuffix(h, ".azurecr.io"):
		return LoginHint{Provider: "Azure Container Registry",
			Command: "az acr login --name " + strings.TrimSuffix(h, ".azurecr.io"),
			Note:    "Signs the engine in with your Azure CLI login."}, true
	case strings.HasSuffix(h, "-docker.pkg.dev"), h == "gcr.io", strings.HasSuffix(h, ".gcr.io"):
		return LoginHint{Provider: "Google Artifact Registry",
			Command: "gcloud auth configure-docker " + h,
			Note:    "Adds the gcloud credential helper for this host to the engine's config."}, true
	case h == "ghcr.io":
		return LoginHint{Provider: "GitHub Container Registry",
			Note: "Use your GitHub user name and a personal access token with read:packages (write:packages to push)."}, true
	}
	return LoginHint{}, false
}
