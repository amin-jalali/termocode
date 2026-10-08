package lspinstall

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
)

// StepKind is what one install step does.
type StepKind int

const (
	// StepRun runs Argv (with Env appended to the environment).
	StepRun StepKind = iota
	// StepDownload fetches URL and unpacks it (Archive) into Dest.
	StepDownload
	// StepRustupWhich resolves `rustup which <Bin>` into the shim target.
	StepRustupWhich
)

// Step is one unit of an install Plan.
type Step struct {
	Kind    StepKind
	Label   string // human description, also the progress line
	Argv    []string
	Env     []string
	URL     string
	Archive string // "zip" | "tar.gz" | "gz" | "raw"
	Dest    string
}

// Plan is the full, pure description of how to install a tool on a host.
type Plan struct {
	Tool     Tool
	Dir      string   // tools/<name>
	Requires []string // host executables that must be on PATH
	Steps    []Step
	// Exec is the expanded shim argv. Empty for KindRustup (resolved by the
	// StepRustupWhich step at install time).
	Exec []string
}

// MissingToolchainError: a host executable the recipe needs is absent.
type MissingToolchainError struct {
	Tool string
	Bin  string
	Hint string
}

func (e *MissingToolchainError) Error() string {
	return fmt.Sprintf("%s needs %s — %s", e.Tool, e.Bin, e.Hint)
}

// UnsupportedError: no automatic recipe for this OS / arch.
type UnsupportedError struct {
	Tool     string
	Platform string
	Hint     string
}

func (e *UnsupportedError) Error() string {
	msg := fmt.Sprintf("no automatic %s install for %s", e.Tool, e.Platform)
	if e.Hint != "" {
		msg += " — " + e.Hint
	}
	return msg
}

// OfflineError: the install failed because the network is unreachable.
type OfflineError struct {
	Tool string
	Err  error
}

func (e *OfflineError) Error() string {
	return fmt.Sprintf("%s: network unreachable (offline?)", e.Tool)
}

func (e *OfflineError) Unwrap() error { return e.Err }

// HostHint is the precise "how do I get this toolchain" text for a host
// executable a recipe needs.
func HostHint(bin string) string {
	switch bin {
	case "go":
		return "install Go from https://go.dev/dl, then retry"
	case "npm", "node":
		return "install Node.js + npm from https://nodejs.org (or your package manager), then retry"
	case "python3":
		return "install Python 3 (e.g. sudo apt install python3 python3-venv), then retry"
	case "cargo", "rustup":
		return "install Rust via https://rustup.rs, then retry"
	}
	return "install " + bin + " and make sure it is on PATH, then retry"
}

// kindRequires is the host toolchain a kind needs.
func kindRequires(k InstallKind) []string {
	switch k {
	case KindGo:
		return []string{"go"}
	case KindNpm:
		return []string{"npm"}
	case KindPip:
		return []string{"python3"}
	case KindCargo:
		return []string{"cargo"}
	case KindRustup:
		return []string{"rustup"}
	}
	return nil
}

// platformToken maps a host name through m. nil map → identity. ok=false
// when the map exists but has no entry (unsupported).
func platformToken(m map[string]string, key string) (string, bool) {
	if m == nil {
		return key, true
	}
	v, ok := m[key]
	return v, ok
}

// ReleaseURL returns the GitHub release download URL of t for goos/goarch.
func ReleaseURL(t Tool, goos, goarch string) (string, error) {
	if t.InstallKind != KindGitHubRelease {
		return "", fmt.Errorf("%s is not a github-release tool", t.Name)
	}
	plat := goos + "/" + goarch
	unsupported := &UnsupportedError{Tool: t.Name, Platform: plat, Hint: t.Fallback}
	if len(t.Platforms) > 0 {
		ok := false
		for _, p := range t.Platforms {
			if p == plat {
				ok = true
				break
			}
		}
		if !ok {
			return "", unsupported
		}
	}
	osTok, ok := platformToken(t.OS, goos)
	if !ok {
		return "", unsupported
	}
	archTok, ok := platformToken(t.Arch, goarch)
	if !ok {
		return "", unsupported
	}
	r := strings.NewReplacer("{version}", t.Version, "{os}", osTok, "{arch}", archTok)
	tag := r.Replace(t.Tag)
	asset := r.Replace(t.Asset)
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", t.Spec, tag, asset), nil
}

// archiveType derives the extractor from an asset/URL name.
func archiveType(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return "zip"
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return "tar.gz"
	case strings.HasSuffix(lower, ".gz"):
		return "gz"
	}
	return "raw"
}

// defaultExec is the kind's natural location for Bin inside dir.
func defaultExec(t Tool, dir string) []string {
	switch t.InstallKind {
	case KindGo, KindCargo:
		return []string{filepath.Join(dir, "bin", t.Bin)}
	case KindNpm:
		return []string{filepath.Join(dir, "node_modules", ".bin", t.Bin)}
	case KindPip:
		return []string{filepath.Join(dir, "venv", "bin", t.Bin)}
	case KindGitHubRelease:
		return []string{filepath.Join(dir, t.Bin)}
	}
	return nil
}

func versioned(spec, sep, version string) string {
	if version == "" || version == "latest" {
		if sep == "@" {
			return spec + "@latest"
		}
		return spec
	}
	return spec + sep + version
}

// BuildPlan returns the install plan for t into dir on goos/goarch. Pure.
func BuildPlan(t Tool, dir, goos, goarch string) (Plan, error) {
	if goos == "windows" {
		return Plan{}, &UnsupportedError{Tool: t.Name, Platform: goos + "/" + goarch,
			Hint: "the managed installer supports Linux and macOS"}
	}
	p := Plan{Tool: t, Dir: dir}
	p.Requires = append(p.Requires, kindRequires(t.InstallKind)...)
	p.Requires = append(p.Requires, t.Needs...)

	switch t.InstallKind {
	case KindGo:
		pkg := versioned(t.Spec, "@", t.Version)
		p.Steps = []Step{{
			Kind: StepRun, Label: "go install " + pkg,
			Argv: []string{"go", "install", pkg},
			Env:  []string{"GOBIN=" + filepath.Join(dir, "bin")},
		}}
	case KindNpm:
		args := []string{"npm", "install", "--prefix", dir, "--no-fund", "--no-audit", "--loglevel=error",
			versioned(t.Spec, "@", t.Version)}
		args = append(args, t.Extra...)
		p.Steps = []Step{{Kind: StepRun, Label: "npm install " + strings.Join(append([]string{versioned(t.Spec, "@", t.Version)}, t.Extra...), " "), Argv: args}}
	case KindPip:
		venv := filepath.Join(dir, "venv")
		py := filepath.Join(venv, "bin", "python")
		pkg := versioned(t.Spec, "==", t.Version)
		p.Steps = []Step{
			{Kind: StepRun, Label: "python3 -m venv", Argv: []string{"python3", "-m", "venv", venv}},
			{Kind: StepRun, Label: "pip install " + pkg, Argv: []string{py, "-m", "pip", "install", "--disable-pip-version-check", "--upgrade", pkg}},
		}
	case KindCargo:
		args := []string{"cargo", "install", "--root", dir, t.Spec}
		if t.Version != "" && t.Version != "latest" {
			args = append(args, "--version", t.Version)
		}
		args = append(args, t.Extra...)
		p.Steps = []Step{{Kind: StepRun, Label: "cargo install " + t.Spec, Argv: args}}
	case KindRustup:
		p.Steps = []Step{
			{Kind: StepRun, Label: "rustup component add " + t.Spec, Argv: []string{"rustup", "component", "add", t.Spec}},
			{Kind: StepRustupWhich, Label: "rustup which " + t.Bin, Argv: []string{"rustup", "which", t.Bin}},
		}
	case KindGitHubRelease:
		url, err := ReleaseURL(t, goos, goarch)
		if err != nil {
			return Plan{}, err
		}
		dest := dir
		if archiveType(url) == "gz" || archiveType(url) == "raw" {
			dest = filepath.Join(dir, t.Bin)
		}
		p.Steps = []Step{{Kind: StepDownload, Label: "download " + filepath.Base(url), URL: url, Archive: archiveType(url), Dest: dest}}
	default:
		return Plan{}, fmt.Errorf("unknown install kind %d", t.InstallKind)
	}

	if t.InstallKind != KindRustup {
		if len(t.Exec) > 0 {
			for _, a := range t.Exec {
				p.Exec = append(p.Exec, t.expand(a, dir))
			}
		} else {
			p.Exec = defaultExec(t, dir)
		}
	}
	return p, nil
}

// HumanCommand is a one-line description of the plan (for confirm dialogs
// and `termocode setup` hints).
func (p Plan) HumanCommand() string {
	labels := make([]string, 0, len(p.Steps))
	for _, s := range p.Steps {
		if s.Kind == StepRustupWhich {
			continue
		}
		labels = append(labels, s.Label)
	}
	return strings.Join(labels, " && ")
}

// offlineMarkers are substrings that go / npm / pip / cargo / curl print
// when DNS or the network is down.
var offlineMarkers = []string{
	"no such host",
	"temporary failure in name resolution",
	"could not resolve host",
	"network is unreachable",
	"getaddrinfo",
	"enotfound",
	"eai_again",
	"econnrefused",
	"etimedout",
	"dial tcp",
	"failed to establish a new connection",
	"name or service not known",
	"connection timed out",
	"no route to host",
	"spurious network error",
}

// IsOfflineText reports whether installer output looks like a network
// failure.
func IsOfflineText(out string) bool {
	lower := strings.ToLower(out)
	for _, m := range offlineMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// ClassifyFailure turns a raw install failure into a typed error:
// *OfflineError for network problems, *MissingToolchainError for a missing
// python venv module, otherwise err unchanged.
func ClassifyFailure(tool, output string, err error) error {
	if err == nil {
		return nil
	}
	var typed *MissingToolchainError
	if errors.As(err, &typed) {
		return err
	}
	var unsup *UnsupportedError
	if errors.As(err, &unsup) {
		return err
	}
	var off *OfflineError
	if errors.As(err, &off) {
		return err
	}
	var netErr net.Error
	var dnsErr *net.DNSError
	var opErr *net.OpError
	if errors.As(err, &dnsErr) || errors.As(err, &opErr) || errors.As(err, &netErr) {
		return &OfflineError{Tool: tool, Err: err}
	}
	if IsOfflineText(output) || IsOfflineText(err.Error()) {
		return &OfflineError{Tool: tool, Err: err}
	}
	lower := strings.ToLower(output)
	if strings.Contains(lower, "ensurepip is not available") || strings.Contains(lower, "no module named venv") {
		return &MissingToolchainError{Tool: tool, Bin: "python3-venv",
			Hint: "install the venv module (e.g. sudo apt install python3-venv), then retry"}
	}
	return err
}
