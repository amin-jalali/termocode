package assets

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The browser code never builds HTML from strings and never runs code
// from strings (never-do 20). These rules scan our own sources; the built
// bundle gets the rules that still hold with the vendored library in it.

type rule struct {
	name  string
	re    *regexp.Regexp
	allow map[string]bool // source files (relative to src/) that may match, with a reason in the comment
}

var sourceRules = []rule{
	{name: "innerHTML and friends", re: regexp.MustCompile(`\b(innerHTML|outerHTML|insertAdjacentHTML|createContextualFragment|DOMParser)\b`)},
	// The one file that may name the prop is the guard that throws on it.
	{name: "dangerouslySetInnerHTML", re: regexp.MustCompile(`dangerouslySetInnerHTML`), allow: map[string]bool{"lib/safety.ts": true}},
	{name: "eval", re: regexp.MustCompile(`\beval\s*\(`)},
	{name: "Function constructor", re: regexp.MustCompile(`\bFunction\s*\(`)},
	{name: "document.write", re: regexp.MustCompile(`document\.write`)},
	{name: "string timer", re: regexp.MustCompile(`set(Timeout|Interval)\s*\(\s*["'\x60]`)},
	{name: "javascript: URL", re: regexp.MustCompile(`(?i)javascript:`)},
	{name: "external URL", re: regexp.MustCompile(`(?i)\b(https?|wss?)://`)},
	// The one dynamic import loads the editor file (WEB-39), a fixed path.
	{name: "dynamic import", re: regexp.MustCompile(`\bimport\s*\(`), allow: map[string]bool{"lib/lazy.ts": true}},
	{name: "remote script/style", re: regexp.MustCompile(`(?i)@import|url\(\s*["']?(https?:)?//`)},
	// Only the prefs module touches localStorage, and it never holds a secret.
	{name: "localStorage", re: regexp.MustCompile(`\blocalStorage\b`), allow: map[string]bool{"lib/prefs.ts": true}},
	// Only the auth module keeps the session token, in sessionStorage.
	{name: "sessionStorage", re: regexp.MustCompile(`\bsessionStorage\b`), allow: map[string]bool{"net/auth.ts": true}},
	// Network: only the auth module fetches (POST /auth), only the link opens a socket.
	{name: "fetch", re: regexp.MustCompile(`\bfetch\s*\(`), allow: map[string]bool{"net/auth.ts": true}},
	{name: "WebSocket", re: regexp.MustCompile(`\bnew\s+(this\.)?(WebSocket|WS)\b|\bWebSocket\s*\(`), allow: map[string]bool{"net/link.ts": true}},
	{name: "cookies", re: regexp.MustCompile(`document\.cookie`)},
	// The terminal's paste sheet takes the browser's own paste; the page
	// never reads the clipboard (web UX 6.3).
	{name: "clipboard read", re: regexp.MustCompile(`\bclipboard\.read(Text)?\b`)},
	// OS notifications come only from lib/notify.ts, which shows fixed
	// texts from a closed list (WEB-37, never-do 14).
	{name: "notifications", re: regexp.MustCompile(`\bNotification\b|\brequestPermission\b|\bshowNotification\b`), allow: map[string]bool{"lib/notify.ts": true}},
	// No service worker, ever (WEB-35): no code of the page may outlive it
	// in the browser, so an old or poisoned copy can never run.
	{name: "service worker", re: regexp.MustCompile(`\bserviceWorker\b|\bServiceWorker\b|\bPushManager\b`)},
	// Messages only between the page and the desktop editor's frame, both
	// sides checking the other's origin and window (monaco/protocol.ts).
	{name: "postMessage/window.open", re: regexp.MustCompile(`\b(postMessage|window\.open)\s*\(`), allow: map[string]bool{"lib/deskeditor.ts": true, "monaco/frame.ts": true}},
	// Names from the hub or the URL (folders, topics, query keys) are never
	// keys of a plain object: "__proto__" would swap its prototype and
	// "constructor" would read Object's. Tables built from data are a Map
	// or a dict (state/dict.ts, no prototype), read with own/has (T49).
	{name: "plain object as a table", re: regexp.MustCompile(`Record<string,[^=;]*>\s*=\s*\{\s*(\}|\.\.\.)|\{\s*\[\w+:\s*string\]:[^=;]*\}\s*=\s*\{\s*(\}|\.\.\.)`)},
	{name: "the in operator on a table", re: regexp.MustCompile(`[(!&|]\s*[A-Za-z_$][\w$]*\s+in\s+[A-Za-z_$][\w$.]*\s*\)|\bfor\s*\(\s*(const|let|var)\s+[\w$]+\s+in\s`)},
}

// lintSource returns the rules text breaks; rel is the path under src/.
func lintSource(rel, text string) []string {
	var bad []string
	for _, r := range sourceRules {
		if r.allow[rel] {
			continue
		}
		if loc := r.re.FindStringIndex(text); loc != nil {
			line := strings.Count(text[:loc[0]], "\n") + 1
			bad = append(bad, rel+":"+strconv.Itoa(line)+": "+r.name)
		}
	}
	return bad
}

// lintSVG keeps static icons inert: no script, no handlers, no links,
// no foreign content; the svg namespace is the only URL.
func lintSVG(text string) string {
	text = strings.Replace(text, `xmlns="http://www.w3.org/2000/svg"`, "", 1)
	for _, r := range []*regexp.Regexp{
		regexp.MustCompile(`(?i)<script|<foreignObject|<use\b|<image\b|\bon[a-z]+\s*=|href|(?:https?|data|javascript):|@import|url\(`),
	} {
		if m := r.FindString(text); m != "" {
			return "svg has " + m
		}
	}
	return ""
}

func TestLintSVG(t *testing.T) {
	for _, bad := range []string{`<svg><script>x</script></svg>`, `<svg onload="x()"/>`, `<svg><use href="#a"/></svg>`, `<svg><image href="data:x"/></svg>`} {
		if lintSVG(bad) == "" {
			t.Errorf("lintSVG passed %q", bad)
		}
	}
}

func TestLintCatchesEachRule(t *testing.T) {
	for _, bad := range []string{
		`el.innerHTML = x`, `<div dangerouslySetInnerHTML={{__html: x}} />`, `eval("1")`,
		`new Function("return 1")`, `document.write("x")`, `setTimeout("go()", 1)`,
		`a.href = "javascript:void 0"`, `fetch("https://example.com")`, `await import("./x")`,
		`@import "x.css";`, `localStorage.setItem("t", tok)`, `sessionStorage.x = 1`,
		`document.cookie = "a=b"`, `window.open("/")`, `new DOMParser()`,
		`navigator.serviceWorker.register("/sw.js")`, `new PushManager()`,
		`new Notification("x", {body: msg})`, `Notification.requestPermission()`, `reg.showNotification("x")`,
		`fetch("/x")`, `new WebSocket("/ws")`, `navigator.clipboard.readText()`,
		`const dirs: Record<string, TreeDir> = { ...s.dirs };`, `const q: Record<string, string> = {};`,
		`const o: { [k: string]: number } = {};`, `if (p in dirs) x();`, `return !(t in seqs);`, `for (const k in obj) f(k);`,
	} {
		if len(lintSource("views/x.tsx", bad)) == 0 {
			t.Errorf("lint passed %q", bad)
		}
	}
	if got := lintSource("views/x.tsx", "const a = h(\"p\", null, text);\nel.textContent = s;\nconst d = dict<number>();\nif (!(\"data\" in v)) f();\nconst t: Record<string, number> = { \"--x\": 1 };"); len(got) != 0 {
		t.Errorf("lint refused clean code: %v", got)
	}
}

func TestSourcesFollowTheBrowserRules(t *testing.T) {
	n := 0
	err := filepath.WalkDir("src", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch filepath.Ext(path) {
		case ".ts", ".tsx", ".css", ".html", ".webmanifest":
		case ".svg":
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bad := lintSVG(string(b)); bad != "" {
				t.Errorf("%s: %s", path, bad)
			}
			return nil
		default:
			t.Errorf("%s: unexpected file kind in src", path)
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("src", path)
		for _, bad := range lintSource(filepath.ToSlash(rel), string(b)) {
			t.Error(bad)
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no sources scanned")
	}
}

// The vendored library names a few XML namespaces; they are identifiers,
// never fetched.
var namespaceURL = regexp.MustCompile(`http://www\.w3\.org/(1998/Math/MathML|1999/xhtml|2000/svg|1999/xlink|XML/1998/namespace)`)

func TestBundleRunsNoStringCodeAndFetchesNothing(t *testing.T) {
	for _, e := range Lock() {
		if filepath.Ext(e.Path) != ".js" && filepath.Ext(e.Path) != ".css" {
			continue
		}
		b, err := os.ReadFile(e.Path) // the committed files, the kit included
		if err != nil {
			t.Fatal(err)
		}
		text := namespaceURL.ReplaceAllString(string(b), "")
		if strings.HasPrefix(e.Path, "monaco/") {
			// The vendored desktop editor, byte for byte (vendor.json). Its
			// help texts name web pages and its worker starter (a text it
			// never runs here) imports a script; its frame's policy
			// (webgw.FrameCSP) has no network and no workers, so neither can
			// load anything. The other rules hold for it too.
			text = regexp.MustCompile(`(?i)(https?|wss?):\/\/|\bimport\s*\(`).ReplaceAllString(text, "")
		}
		if e.Path == "app.js" { // its one dynamic import: the editor file, a fixed path
			text = strings.Replace(text, `import("/assets/editor.js")`, "", 1)
		}
		for _, r := range []*regexp.Regexp{
			regexp.MustCompile(`\beval\s*\(`), regexp.MustCompile(`\bnew Function\b|\bFunction\s*\(\s*["'\x60]`),
			regexp.MustCompile(`document\.write`), regexp.MustCompile(`\bimport\s*\(`),
			regexp.MustCompile(`(?i)(https?|wss?):\/\/`), regexp.MustCompile(`(?i)@import`),
			regexp.MustCompile(`(?i)sourceMappingURL`), regexp.MustCompile(`\bserviceWorker\b`),
		} {
			if loc := r.FindStringIndex(text); loc != nil {
				lo, hi := max(0, loc[0]-40), min(len(text), loc[1]+40)
				t.Errorf("%s matches %s near %q", e.Path, r, text[lo:hi])
			}
		}
	}
}

func TestPagesHaveNoInlineCodeOrStyle(t *testing.T) {
	scriptTag := regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script>`)
	for _, e := range Lock() {
		if filepath.Ext(e.Path) != ".html" {
			continue
		}
		b, err := os.ReadFile(e.Path) // the committed files, the kit included
		if err != nil {
			t.Fatal(err)
		}
		html := string(b)
		tags := scriptTag.FindAllStringSubmatch(html, -1)
		if len(tags) == 0 {
			t.Errorf("%s has no script tag", e.Path)
		}
		for _, m := range tags {
			if strings.TrimSpace(m[2]) != "" || !strings.Contains(m[1], `src="/assets/`) || !strings.Contains(m[1], `integrity="sha384-`) {
				t.Errorf("%s: script tag %q is inline or has no SRI", e.Path, m[0])
			}
		}
		for _, r := range []*regexp.Regexp{
			regexp.MustCompile(`(?i)<style\b`), regexp.MustCompile(`(?i)\sstyle\s*=`),
			regexp.MustCompile(`(?i)\son[a-z]+\s*=`), regexp.MustCompile(`(?i)(https?|wss?):\/\/`),
			regexp.MustCompile(`(?i)<(iframe|object|embed|base|form)\b`),
		} {
			if r.MatchString(html) {
				t.Errorf("%s matches %s", e.Path, r)
			}
		}
	}
}

// esbuild drops type-only imports without reading them and does not type
// check, so a wrong path would pass the build silently. Every relative
// import must name a real file; leaving src/ is allowed only for the
// generated protocol types, and only as a type import.
func TestRelativeImportsResolve(t *testing.T) {
	imp := regexp.MustCompile(`(?m)^\s*(import\s+type\b)?[^"'\n]*?\bfrom\s+["'](\.[^"']+)["']|^\s*import\s+["'](\.[^"']+)["']`)
	srcAbs, _ := filepath.Abs("src")
	protoAbs, _ := filepath.Abs("../../webproto/proto")
	err := filepath.WalkDir("src", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || (filepath.Ext(path) != ".ts" && filepath.Ext(path) != ".tsx") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Multi-line import lists: join each import statement onto one line.
		text := regexp.MustCompile(`(?s)import\s*(type\s*)?\{[^}]*\}`).ReplaceAllStringFunc(string(b), func(s string) string {
			return strings.Join(strings.Fields(s), " ")
		})
		for _, m := range imp.FindAllStringSubmatch(text, -1) {
			spec := m[2] + m[3]
			target, _ := filepath.Abs(filepath.Join(filepath.Dir(path), spec))
			found := false
			for _, ext := range []string{"", ".ts", ".tsx", ".d.ts"} {
				if st, err := os.Stat(target + ext); err == nil && !st.IsDir() {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: import %q names no file", path, spec)
			}
			inside := strings.HasPrefix(target, srcAbs+string(filepath.Separator))
			if !inside && (target != protoAbs || m[1] == "") {
				t.Errorf("%s: import %q leaves src/ (only `import type` of webproto/proto may)", path, spec)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The store and the guard are pure logic: no DOM, no timers, no network.
// That keeps them replayable against the Go goldens in any JS engine
// (TestStoreReplayMatchesGo) and keeps time and I/O in one place (net/).
func TestStoreAndGuardArePure(t *testing.T) {
	impure := regexp.MustCompile(`\b(window|document|navigator|location|history|localStorage|sessionStorage|WebSocket|fetch|setTimeout|setInterval|requestAnimationFrame|Date\.now|performance|Math\.random)\b`)
	for _, dir := range []string{"src/state", "src/proto"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			// Comments may name these words.
			code := regexp.MustCompile(`(?m)//.*$|(?s)/\*.*?\*/`).ReplaceAllString(string(b), "")
			if m := impure.FindString(code); m != "" {
				t.Errorf("%s/%s uses %s; keep state and proto pure", dir, e.Name(), m)
			}
		}
	}
}
