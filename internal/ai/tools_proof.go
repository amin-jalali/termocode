package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// Proof of work (CMP-52): proof_screenshot opens a page in a headless
// browser (Chrome, Chromium, Edge or Brave found on this machine) and
// saves a PNG under .termocode/proof/ with a side file that says what
// was shot. The model sees the picture when it takes images, so it can
// check its own UI change; the user sees every shot in AI: Proof of
// Work…. The browser gets a fresh throw-away profile each time (no
// cookies, no sign-in of the user) and runs with its sandbox on.

// ProofDir is where proof screenshots go, relative to the workspace.
const ProofDir = ".termocode/proof"

// proofTimeout caps one browser run.
const proofTimeout = 60 * time.Second

// Seams for tests.
var (
	proofLookPath = exec.LookPath
	proofNow      = time.Now
)

// proofBrowsers are the browser commands looked up on PATH, in order.
var proofBrowsers = []string{
	"chromium", "chromium-browser", "google-chrome", "google-chrome-stable",
	"chrome", "microsoft-edge", "microsoft-edge-stable", "brave-browser", "brave",
}

// proofMacApps are the macOS app bundles tried after PATH.
var proofMacApps = []string{
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	"/Applications/Chromium.app/Contents/MacOS/Chromium",
	"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
	"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
}

// Proof is one saved proof screenshot.
type Proof struct {
	Path   string    `json:"-"` // the PNG, absolute
	Rel    string    `json:"-"` // the PNG, relative to the workspace
	URL    string    `json:"url"`
	Width  int       `json:"width"`
	Height int       `json:"height"`
	Note   string    `json:"note,omitempty"`
	Time   time.Time `json:"time"`
}

// FindProofBrowser returns the headless browser to use: the
// TERMOCODE_BROWSER variable, else the first known one on PATH (and on
// macOS the usual app bundles); "" when there is none.
func FindProofBrowser() string {
	if v := strings.TrimSpace(os.Getenv("TERMOCODE_BROWSER")); v != "" {
		return v
	}
	for _, n := range proofBrowsers {
		if p, err := proofLookPath(n); err == nil {
			return p
		}
	}
	if runtime.GOOS == "darwin" {
		for _, p := range proofMacApps {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p
			}
		}
	}
	return ""
}

// errNoProofBrowser says how to get a browser.
var errNoProofBrowser = errors.New("no headless browser found (Chrome, Chromium, Edge or Brave): install one, or set TERMOCODE_BROWSER to its program path")

// proofTarget checks the page to shoot. An http(s) address is local
// when its host is a loopback name or address; a path is a file inside
// the workspace (never a secret file) and is opened as file://. Every
// other scheme is refused.
func proofTarget(w Workspace, raw string) (target string, local bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, errors.New("url is empty: give the page address (http://localhost:5173/) or an HTML file in the workspace")
	}
	if u, perr := url.Parse(raw); perr == nil && u.Scheme != "" && !(len(u.Scheme) == 1 && runtime.GOOS == "windows") {
		switch strings.ToLower(u.Scheme) {
		case "http", "https":
		default:
			return "", false, fmt.Errorf("only http, https or a workspace file can be shot, not %s:", u.Scheme)
		}
		if u.Host == "" {
			return "", false, errors.New("the address has no host")
		}
		return u.String(), proofLoopback(u.Hostname()), nil
	}
	abs, err := w.Resolve(raw)
	if err != nil {
		return "", false, err
	}
	rel := w.rel(abs)
	if SecretFile(abs) {
		return "", false, errSecretFile(rel)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", false, fmt.Errorf("%s: no such file in the workspace", rel)
	}
	if fi.IsDir() {
		return "", false, fmt.Errorf("%s is a folder: name an HTML file", rel)
	}
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".html", ".htm", ".xhtml", ".svg":
	default:
		return "", false, fmt.Errorf("%s is not a page: only .html, .htm, .xhtml or .svg files (or an http address) can be shot", rel)
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String(), true, nil
}

// proofLoopback reports a loopback host (localhost, 127.x, ::1).
func proofLoopback(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(host), ".")
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// proofBrowserArgs are the headless browser's arguments.
func proofBrowserArgs(target, out, profile string, width, height int) []string {
	return []string{
		"--headless=new",
		"--disable-gpu",
		"--hide-scrollbars",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
		"--disable-sync",
		"--mute-audio",
		"--user-data-dir=" + profile,
		"--window-size=" + strconv.Itoa(width) + "," + strconv.Itoa(height),
		"--virtual-time-budget=5000", // let the page's scripts run before the shot
		"--screenshot=" + out,
		target,
	}
}

// proofSize clamps a width or height (0 = the default).
func proofSize(v, def, lo, hi int) int {
	if v <= 0 {
		return def
	}
	return min(max(v, lo), hi)
}

// proofSlug is a short file-name part from the label or the address.
func proofSlug(label, target string) string {
	s := label
	if s == "" {
		if u, err := url.Parse(target); err == nil {
			s = u.Hostname() + u.Path
			if u.Scheme == "file" {
				s = filepath.Base(u.Path)
			}
		}
	}
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 40 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "page"
	}
	return out
}

// ProofShotTool is proof_screenshot. browser returns the browser
// program ("" none); it is asked at each call, so installing one later
// works without a restart.
func ProofShotTool(w Workspace, browser func() string) Tool {
	gate := func(a map[string]any) (bool, string) {
		if _, local, err := proofTarget(w, ArgString(a, "url")); err == nil && local {
			return false, ""
		}
		return true, ClassRun
	}
	return Tool{
		Name: "proof_screenshot",
		Description: "Take a screenshot of a web page in a headless browser as proof of a UI change: the local dev server " +
			"(http://localhost:<port>/…) or an HTML file in the workspace. The PNG is saved under " + ProofDir + "/ (the user sees " +
			"every shot in AI: Proof of Work…) and you see the picture when your model takes images. After a UI change, shoot " +
			"the changed page at desktop width (1280) and at phone width (390) and say what the shots show. Start the dev server " +
			"first (terminal_send). A localhost page or a workspace file runs at once; any other address asks the user.",
		Params: map[string]any{
			"url":    strParam("page address (http://localhost:5173/settings) or an HTML file relative to the workspace"),
			"width":  map[string]any{"type": "integer", "description": "window width in px (default 1280; 390 for a phone)"},
			"height": map[string]any{"type": "integer", "description": "window height in px (default 800)"},
			"label":  strParam("short name for the file (optional, e.g. settings-phone)"),
			"note":   strParam("what this shot proves, shown to the user (optional)"),
		},
		Required: []string{"url"},
		Validate: func(a map[string]any) error {
			_, _, err := proofTarget(w, ArgString(a, "url"))
			return err
		},
		Gate: gate,
		Describe: func(a map[string]any) string {
			return "Screenshot " + ArgString(a, "url") + " (headless browser, saved to " + ProofDir + ")"
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			return proofShoot(ctx, w, browser(), a)
		},
	}
}

// proofShoot runs the browser once and saves the shot and its side file.
func proofShoot(ctx context.Context, w Workspace, bin string, a map[string]any) (string, error) {
	target, _, err := proofTarget(w, ArgString(a, "url"))
	if err != nil {
		return "", err
	}
	if bin == "" {
		return "", errNoProofBrowser
	}
	width := proofSize(ArgInt(a, "width"), 1280, 320, 3840)
	height := proofSize(ArgInt(a, "height"), 800, 240, 4000)
	root, err := w.Resolve(".")
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, filepath.FromSlash(ProofDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	now := proofNow()
	base := now.Format("20060102-150405") + "-" + proofSlug(strings.TrimSpace(ArgString(a, "label")), target)
	out := filepath.Join(dir, base+".png")
	for i := 2; ; i++ {
		if _, err := os.Stat(out); errors.Is(err, os.ErrNotExist) {
			break
		}
		out = filepath.Join(dir, fmt.Sprintf("%s-%d.png", base, i))
	}
	profile, err := os.MkdirTemp("", "termocode-proof-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(profile)

	rctx, cancel := context.WithTimeout(ctx, proofTimeout)
	defer cancel()
	cmd := spawn.CommandContext(rctx, bin, proofBrowserArgs(target, out, profile, width, height)...)
	cmd.Env = NonInteractiveEnv(os.Environ())
	cmd.Dir = root
	msg, runErr := cmd.CombinedOutput()
	fi, statErr := os.Stat(out)
	if statErr != nil || fi.Size() == 0 {
		_ = os.Remove(out)
		tail := strings.TrimSpace(capOut(Redact(string(msg)), 1500))
		switch {
		case rctx.Err() == context.DeadlineExceeded:
			return "", fmt.Errorf("the browser took over %s and saved no picture", proofTimeout)
		case runErr != nil:
			return "", fmt.Errorf("the browser failed (%v) and saved no picture: %s", runErr, tail)
		}
		return "", fmt.Errorf("the browser saved no picture: %s", tail)
	}
	p := Proof{URL: target, Width: width, Height: height, Note: strings.TrimSpace(ArgString(a, "note")), Time: now}
	if b, err := json.MarshalIndent(p, "", "  "); err == nil {
		_ = os.WriteFile(strings.TrimSuffix(out, ".png")+".json", b, 0o644)
	}
	rel := w.rel(out)
	res := fmt.Sprintf("Saved proof screenshot %s (%d×%d) of %s.", rel, width, height, target)
	if fi.Size() <= lookMaxImage {
		if data, err := os.ReadFile(out); err == nil && AttachImages(ctx, Image{MediaType: "image/png", Data: data}) {
			return res + " The picture is attached: check it before you say the change works.", nil
		}
	}
	return res + " Your model does not see images here: use look_at on the file to check it.", nil
}

// ListProofs returns the proof screenshots of the workspace at root,
// newest first.
func ListProofs(root string) ([]Proof, error) {
	dir := filepath.Join(root, filepath.FromSlash(ProofDir))
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Proof
	for _, e := range ents {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".png") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		var p Proof
		if b, err := os.ReadFile(strings.TrimSuffix(path, filepath.Ext(path)) + ".json"); err == nil {
			_ = json.Unmarshal(b, &p)
		}
		if p.Time.IsZero() {
			if fi, err := e.Info(); err == nil {
				p.Time = fi.ModTime()
			}
		}
		p.Path = path
		p.Rel = filepath.ToSlash(filepath.Join(ProofDir, e.Name()))
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Time.Equal(out[j].Time) {
			return out[i].Time.After(out[j].Time)
		}
		return out[i].Path > out[j].Path
	})
	return out, nil
}

// ClearProofs deletes every proof screenshot and its side file; other
// files in the folder stay. It returns how many shots it removed.
func ClearProofs(root string) (int, error) {
	list, err := ListProofs(root)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range list {
		if err := os.Remove(p.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return n, err
		}
		_ = os.Remove(strings.TrimSuffix(p.Path, filepath.Ext(p.Path)) + ".json")
		n++
	}
	return n, nil
}
