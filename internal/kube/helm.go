package kube

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
)

// helmWriteTimeout bounds a rollback (it waits for the pods) or an
// uninstall.
const helmWriteTimeout = 5 * time.Minute

// Helm runs the helm CLI for one kube context and namespace. Reads are
// list, status, values and history; the only writes are Rollback and
// Uninstall, which the app runs after a Cancel-first card (the AI never
// runs them). The zero Namespace means all namespaces for Releases.
type Helm struct {
	Binary    string // helm's path ("" = not installed)
	Context   string // --kube-context ("" = the kubeconfig's current one)
	Namespace string
	X         services.Exec // nil = services.SpawnExec{}
	Timeout   time.Duration // per read; 0 = DefaultTimeout
}

// FindHelm looks helm up on PATH. lookPath nil = exec.LookPath.
func FindHelm(lookPath func(string) (string, error)) string {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	p, err := lookPath("helm")
	if err != nil {
		return ""
	}
	return p
}

// Release is one row of `helm list`.
type Release struct {
	Name       string
	Namespace  string
	Revision   int
	Updated    string
	Status     string // deployed, failed, pending-upgrade, …
	Chart      string // name-version
	AppVersion string
}

// ReleaseStatus is `helm status` of one release.
type ReleaseStatus struct {
	Name          string
	Namespace     string
	Revision      int
	Status        string
	Description   string
	FirstDeployed string
	LastDeployed  string
	Chart         string
	AppVersion    string
	Notes         string
}

// Revision is one row of `helm history`.
type Revision struct {
	Revision    int
	Updated     string
	Status      string
	Chart       string
	AppVersion  string
	Description string
}

func (h Helm) exec() services.Exec {
	if h.X == nil {
		return services.SpawnExec{}
	}
	return h.X
}

// scope is --kube-context / --namespace; allNS asks for every namespace
// when the session has none.
func (h Helm) scope(allNS bool) []string {
	var out []string
	if h.Context != "" {
		out = append(out, "--kube-context", h.Context)
	}
	switch {
	case h.Namespace != "":
		out = append(out, "--namespace", h.Namespace)
	case allNS:
		out = append(out, "--all-namespaces")
	}
	return out
}

func (h Helm) capture(parent context.Context, d time.Duration, args ...string) (string, error) {
	return h.captureScoped(parent, d, false, args...)
}

func (h Helm) captureScoped(parent context.Context, d time.Duration, allNS bool, args ...string) (string, error) {
	if h.Binary == "" {
		return "", &Error{Kind: ErrNoHelm}
	}
	if d <= 0 {
		d = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(parent, d)
	defer cancel()
	out, stderr, code, err := h.exec().Capture(ctx, "", append([]string{h.Binary}, append(args, h.scope(allNS)...)...)...)
	if err != nil || code != 0 {
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		e := ErrorLine(stderr, err)
		if KindOf(e) == ErrNoKubectl {
			return "", &Error{Kind: ErrNoHelm}
		}
		return "", e
	}
	return out, nil
}

// releaseName is helm's rule for a release name; it also keeps a name
// from being read as a flag.
var releaseName = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,51}[a-z0-9])?$`)

var errBadRelease = errors.New("not a helm release name")

func checkRelease(name string) error {
	if !releaseName.MatchString(name) {
		return errBadRelease
	}
	return nil
}

// Releases is `helm list` of the namespace (all namespaces when none).
func (h Helm) Releases(ctx context.Context) ([]Release, error) {
	out, err := h.captureScoped(ctx, h.Timeout, true, "list", "-o", "json")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Name       string `json:"name"`
		Namespace  string `json:"namespace"`
		Revision   string `json:"revision"`
		Updated    string `json:"updated"`
		Status     string `json:"status"`
		Chart      string `json:"chart"`
		AppVersion string `json:"app_version"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, &Error{Kind: ErrOther, Line: "helm list: unreadable answer"}
	}
	rs := make([]Release, 0, len(raw))
	for _, r := range raw {
		n, _ := strconv.Atoi(r.Revision)
		rs = append(rs, Release{Name: r.Name, Namespace: r.Namespace, Revision: n, Updated: r.Updated,
			Status: r.Status, Chart: r.Chart, AppVersion: r.AppVersion})
	}
	return rs, nil
}

// Status is `helm status <name>`.
func (h Helm) Status(ctx context.Context, name string) (ReleaseStatus, error) {
	if err := checkRelease(name); err != nil {
		return ReleaseStatus{}, err
	}
	out, err := h.capture(ctx, h.Timeout, "status", name, "-o", "json")
	if err != nil {
		return ReleaseStatus{}, err
	}
	var raw struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Version   int    `json:"version"`
		Info      struct {
			FirstDeployed string `json:"first_deployed"`
			LastDeployed  string `json:"last_deployed"`
			Status        string `json:"status"`
			Description   string `json:"description"`
			Notes         string `json:"notes"`
		} `json:"info"`
		Chart struct {
			Metadata struct {
				Name       string `json:"name"`
				Version    string `json:"version"`
				AppVersion string `json:"appVersion"`
			} `json:"metadata"`
		} `json:"chart"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return ReleaseStatus{}, &Error{Kind: ErrOther, Line: "helm status: unreadable answer"}
	}
	chart := raw.Chart.Metadata.Name
	if v := raw.Chart.Metadata.Version; v != "" {
		chart += "-" + v
	}
	return ReleaseStatus{Name: raw.Name, Namespace: raw.Namespace, Revision: raw.Version,
		Status: raw.Info.Status, Description: raw.Info.Description,
		FirstDeployed: raw.Info.FirstDeployed, LastDeployed: raw.Info.LastDeployed,
		Chart: chart, AppVersion: raw.Chart.Metadata.AppVersion, Notes: raw.Info.Notes}, nil
}

// History is `helm history <name>` (last 50), newest first.
func (h Helm) History(ctx context.Context, name string) ([]Revision, error) {
	if err := checkRelease(name); err != nil {
		return nil, err
	}
	out, err := h.capture(ctx, h.Timeout, "history", name, "-o", "json", "--max", "50")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Revision    int    `json:"revision"`
		Updated     string `json:"updated"`
		Status      string `json:"status"`
		Chart       string `json:"chart"`
		AppVersion  string `json:"app_version"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, &Error{Kind: ErrOther, Line: "helm history: unreadable answer"}
	}
	hs := make([]Revision, 0, len(raw))
	for _, r := range raw {
		hs = append(hs, Revision(r))
	}
	sort.SliceStable(hs, func(i, j int) bool { return hs[i].Revision > hs[j].Revision })
	return hs, nil
}

// Values is `helm get values <name>` as YAML (revision 0 = the current
// one), with secret-looking values masked: it is for reading on screen,
// never for re-applying.
func (h Helm) Values(ctx context.Context, name string, revision int) (string, error) {
	if err := checkRelease(name); err != nil {
		return "", err
	}
	args := []string{"get", "values", name, "-o", "yaml"}
	if revision > 0 {
		args = append(args, "--revision", strconv.Itoa(revision))
	}
	out, err := h.capture(ctx, h.Timeout, args...)
	if err != nil {
		return "", err
	}
	return MaskValues(out), nil
}

// secretKey is a YAML key whose value is likely a secret.
var secretKey = regexp.MustCompile(`(?i)(pass(word|wd)?|secret|token|api[_-]?key|private[_-]?key|credential|auth)`)

// MaskValues replaces the value of every secret-looking YAML key with
// dots; nested maps (a key with no value) are kept as they are.
func MaskValues(yaml string) string {
	lines := strings.Split(yaml, "\n")
	for i, l := range lines {
		k, v, ok := strings.Cut(l, ":")
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		key := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(k), "- "))
		if key == "" || strings.ContainsAny(key, " \"'") || !secretKey.MatchString(key) {
			continue
		}
		lines[i] = k + ": ••••"
	}
	return strings.Join(lines, "\n")
}

// Rollback is `helm rollback <name> <revision> --wait`. The app asks
// the user first (Cancel-first card); the AI never runs it.
func (h Helm) Rollback(ctx context.Context, name string, revision int) error {
	if err := checkRelease(name); err != nil {
		return err
	}
	if revision < 1 {
		return errors.New("rollback needs a revision")
	}
	_, err := h.capture(ctx, helmWriteTimeout, "rollback", name, strconv.Itoa(revision), "--wait")
	return err
}

// Uninstall is `helm uninstall <name>`. The app asks the user first
// (Cancel-first card); the AI never runs it.
func (h Helm) Uninstall(ctx context.Context, name string) error {
	if err := checkRelease(name); err != nil {
		return err
	}
	_, err := h.capture(ctx, helmWriteTimeout, "uninstall", name)
	return err
}
