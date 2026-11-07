package kube

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

// CTX-39: helm repo add and chart install (writes: the app asks first,
// the AI never runs them), and the Kustomize preview (read only).

var (
	repoName     = regexp.MustCompile(`^[a-z0-9]([a-z0-9_.-]{0,62})$`)
	repoURL      = regexp.MustCompile(`^(https?|oci)://[^\s'"]+$`)
	chartRef     = regexp.MustCompile(`^([a-z0-9][a-z0-9_.-]*/[a-z0-9][a-z0-9_.-]*|oci://[^\s'"]+)$`)
	chartVersion = regexp.MustCompile(`^[A-Za-z0-9.+_~^<>=*-]{1,64}$`)
)

// CheckRepo refuses a repo name or URL helm would misread.
func CheckRepo(name, url string) error {
	switch {
	case !repoName.MatchString(name):
		return errors.New("a repo name is lower case letters, digits, '-', '_' or '.'")
	case !repoURL.MatchString(url):
		return errors.New("the repo URL must start with https://, http:// or oci://")
	}
	return nil
}

// CheckChart refuses a release, chart ("repo/name" or "oci://…") or
// version ("" = the latest) helm would misread.
func CheckChart(release, chart, version string) error {
	switch {
	case checkRelease(release) != nil:
		return errBadRelease
	case !chartRef.MatchString(chart):
		return errors.New("the chart is repo/name or oci://…")
	case version != "" && !chartVersion.MatchString(version):
		return errors.New("not a chart version")
	}
	return nil
}

// RepoAdd is `helm repo add <name> <url>`: it changes the user's helm
// repo list (not the cluster).
func (h Helm) RepoAdd(ctx context.Context, name, url string) error {
	if err := CheckRepo(name, url); err != nil {
		return err
	}
	_, err := h.capture(ctx, helmWriteTimeout, "repo", "add", name, url)
	return err
}

// InstallArgs is the helm argv (without the binary and scope) of an
// install, for the card to show.
func InstallArgs(release, chart, version string) []string {
	args := []string{"install", release, chart}
	if version != "" {
		args = append(args, "--version", version)
	}
	return args
}

// Install is `helm install <release> <chart> [--version v]` in the
// session's namespace.
func (h Helm) Install(ctx context.Context, release, chart, version string) (string, error) {
	if err := CheckChart(release, chart, version); err != nil {
		return "", err
	}
	return h.capture(ctx, helmWriteTimeout, InstallArgs(release, chart, version)...)
}

// Kustomize is `kubectl kustomize <dir>`: the manifests a kustomization
// builds, never sent to the cluster. Secret values are masked.
func (c Client) Kustomize(ctx context.Context, dir string) (string, error) {
	if dir == "" || strings.HasPrefix(dir, "-") || !filepath.IsAbs(dir) {
		return "", errors.New("kustomize needs an absolute folder")
	}
	out, err := c.capture(ctx, "kustomize", dir)
	if err != nil {
		return "", err
	}
	docs := strings.Split(out, "\n---\n")
	for i, d := range docs {
		docs[i] = MaskSecretYAML("", d)
	}
	return strings.Join(docs, "\n---\n"), nil
}
