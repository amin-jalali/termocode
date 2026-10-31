//go:build e2e && browser

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// WEB-28: the web UI against the real termocode binary. termocode runs
// in a PTY with "web_panel": "on"; `termocode web start --yes` asks it to
// start the real gateway; a fake browser opener catches the 0600 redirect
// page; headless Chrome opens the one-time link and the test walks what a
// user does on a phone and on a laptop: chat with a (fake) AI that
// streams, answer its card, open a file, type in a terminal, stage and
// commit, and stop the panel from the CLI.
//
// Run: go -C e2e test -tags e2e,browser -run TestWebReal .

// webModel is a scripted OpenAI-compatible model. A request without a
// tool result streams a greeting in two parts and then one write_file
// call; a request that carries the tool result answers webDone.
type webModel struct {
	srv *httptest.Server

	mu     sync.Mutex
	bodies []string
}

const (
	webHello1 = "Hello from the "
	webHello2 = "fake model."
	webDone   = "web-e2e-done-4b2"
	webNotes  = "first note\nwritten from the web\n"
)

func newWebModel(t *testing.T) *webModel {
	t.Helper()
	wm := &webModel{}
	wm.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		body := string(b)
		wm.mu.Lock()
		wm.bodies = append(wm.bodies, body)
		wm.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		f, _ := w.(http.Flusher)
		send := func(chunk any) {
			out, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", out)
			if f != nil {
				f.Flush()
			}
		}
		text := func(s string) any {
			return map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": s}}}}
		}
		if strings.Contains(body, `"role":"tool"`) {
			send(text(webDone))
		} else {
			send(text(webHello1))
			send(text(webHello2))
			a, _ := json.Marshal(map[string]any{"path": "notes.txt", "content": webNotes})
			send(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
				map[string]any{"index": 0, "id": "call_web1", "function": map[string]any{"name": "write_file", "arguments": string(a)}}}}}}})
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(wm.srv.Close)
	return wm
}

func (wm *webModel) env() []string {
	return []string{"TERMOCODE_AI_PROVIDER=ollama", "TERMOCODE_AI_MODEL=fake", "TERMOCODE_AI_BASE_URL=" + wm.srv.URL + "/v1"}
}

// webRig is one termocode with the web panel turned on.
type webRig struct {
	t        *testing.T
	home, ws string
	env      []string
	opened   string // where the fake opener copies the redirect page
	openArgs string // the fake opener's argv, one line per call
}

// fakeOpener is the browser opener termocode calls (xdg-open on Linux,
// open on macOS): it records its arguments and copies the page it got.
const fakeOpener = `#!/bin/sh
printf '%s\n' "$*" >> "$TC_E2E_OPEN_ARGS"
cat "$1" > "$TC_E2E_OPENED.tmp" && mv "$TC_E2E_OPENED.tmp" "$TC_E2E_OPENED"
`

func newWebRig(t *testing.T, extraEnv ...string) *webRig {
	t.Helper()
	rt, err := shortTempDir("tcweb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(rt) })
	home := t.TempDir()
	bin := filepath.Join(home, "fakebin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"xdg-open", "open"} {
		if err := os.WriteFile(filepath.Join(bin, n), []byte(fakeOpener), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(home, ".config", "termocode", "config.json"), `{"web_panel": "on"}`)
	writeFile(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = Web Tester\n\temail = web@example.com\n")
	r := &webRig{t: t, home: home, ws: NewRepo(t),
		opened: filepath.Join(home, "opened.html"), openArgs: filepath.Join(home, "open-args.txt")}
	r.env = append([]string{
		"XDG_RUNTIME_DIR=" + rt,
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"TC_E2E_OPENED=" + r.opened,
		"TC_E2E_OPEN_ARGS=" + r.openArgs,
	}, extraEnv...)
	return r
}

// cli runs `termocode <args>` in the workspace with the rig's env.
func (r *webRig) cli(args ...string) (string, int) {
	r.t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Dir = r.ws
	cmd.Env = append(isolatedEnv(r.home), r.env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		r.t.Fatalf("termocode %v: %v", args, err)
	}
	return out.String(), code
}

var webLinkRE = regexp.MustCompile(`url=([^"]+)"`)

// startPanel runs `termocode web start --yes` (retrying while the hub
// is not up yet) and returns the one-time link from the redirect page.
func (r *webRig) startPanel() string {
	r.t.Helper()
	deadline := time.Now().Add(bootTimeout)
	for {
		out, code := r.cli("web", "start", "--yes")
		if code == 0 {
			if !strings.Contains(out, "The web panel is open at http://127.0.0.1:") {
				r.t.Fatalf("web start said %q", out)
			}
			break
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("web start: exit %d: %s", code, out)
		}
		time.Sleep(200 * time.Millisecond)
	}
	var page []byte
	waitUntil(r.t, "the redirect page at the browser opener", uiTimeout, func() bool {
		b, err := os.ReadFile(r.opened)
		page = b
		return err == nil && len(b) > 0
	})
	m := webLinkRE.FindSubmatch(page)
	if m == nil {
		r.t.Fatalf("no link in the redirect page:\n%s", page)
	}
	link := html.UnescapeString(string(m[1]))
	if !strings.HasPrefix(link, "http://127.0.0.1:") || !strings.Contains(link, "/#s=") {
		r.t.Fatalf("link %q is not a loopback launch link", link)
	}
	// The secret never is a program's argument (every user can read argv).
	args, _ := os.ReadFile(r.openArgs)
	secret := link[strings.Index(link, "#s=")+3:]
	if strings.Contains(string(args), secret) || strings.Contains(string(args), "#s=") {
		r.t.Fatalf("the browser opener got the secret in its argv: %q", args)
	}
	return link
}

func (r *webRig) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.ws
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+r.ws)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

const (
	wChat  = `document.querySelector('[data-view="chat"]')`
	wFiles = `document.querySelector('[data-view="files"]')`
	wTerm  = `document.querySelector('[data-view="term"]')`
)

// goTab taps a tab in the tab bar (phone) or picks the view on the
// desktop workbench: the activity bar's item, or the bottom panel for
// the terminal (which a terminal route opens).
func (p *webPage) goTab(tab string) {
	p.t.Helper()
	if !p.vp.Mobile {
		if tab == "term" {
			webEval[bool](p, `(location.hash = "#/term", true)`)
			p.waitJS(`document.querySelector('.wb-panel [data-view="term"]')`)
			return
		}
		p.tap(fmt.Sprintf(`nav .act-item[data-act="%s"]`, tab))
		p.waitJS(fmt.Sprintf(`document.querySelector('nav .act-item[data-act="%s"][aria-current="page"]') && document.querySelector('[data-view="%s"]')`, tab, tab))
		return
	}
	i := map[string]int{"chat": 1, "files": 2, "term": 3, "git": 4, "more": 5}[tab]
	p.tap(fmt.Sprintf("nav .nav-item:nth-child(%d)", i))
	p.waitJS(fmt.Sprintf(`document.querySelector('nav .nav-item:nth-child(%d)[aria-current="page"]') && document.querySelector('[data-view="%s"]')`, i, tab))
}

func TestWebRealBinary(t *testing.T) {
	for _, vp := range []webViewport{webPhone, webDesktop} {
		t.Run(vp.Name, func(t *testing.T) { webRealWalk(t, vp) })
	}
}

func webRealWalk(t *testing.T, vp webViewport) {
	model := newWebModel(t)
	r := newWebRig(t, model.env()...)
	tm := Start(t, StartOpts{Cols: 140, Rows: 40, Home: r.home, Workspace: r.ws, Env: r.env})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})

	// Start and auth: the link works once; the page trades it for a token.
	link := r.startPanel()
	tm.WaitText("Web panel is on", uiTimeout)
	p := newWebPage(t, vp)
	p.do(chromedp.Navigate(link))
	p.tap(".connect-foot button")
	p.waitJS(`document.documentElement.dataset.link === "online"`)
	p.waitJS(`document.querySelector(".strip .branch")?.textContent.includes("main")`)
	if h := webEval[string](p, `location.hash`); strings.Contains(h, "s=") {
		t.Errorf("the secret is still in the address bar: %q", h)
	}

	// Chat: the answer streams in; the AI's file change comes as a card.
	composer := wChat + `.querySelector(".composer")`
	p.tapJS(composer + `.querySelector("textarea")`)
	p.keys("please add a line to notes")
	p.waitJS(composer + `.querySelector("textarea").value === "please add a line to notes"`)
	p.tapJS(composer + `.querySelector(".send")`)
	p.waitJS(wChat + `.innerText.includes(` + webJSQ(webHello1+webHello2) + `)`)
	// The card waits behind the attention pill; Review opens it.
	p.waitJS(`document.querySelector(".attention.att-cards")?.textContent.includes("1 approval waiting")`)
	p.tap(".attention.att-cards")
	card := `[...document.querySelectorAll(".sheet .card")].find((c) => c.innerText.includes("Change this file?") && c.querySelector(".btn-ai"))`
	p.waitJS(card)
	if first := webEval[string](p, `(`+card+`).querySelector(".card-foot button")?.textContent.trim()`); first != "Reject" {
		t.Errorf("the card's first button is %q, want Reject (safe first)", first)
	}
	p.waitJS(`(` + card + `).querySelector(".btn-ai").classList.contains("is-armed")`)
	p.tapJS(`(` + card + `).querySelector(".btn-ai")`)
	p.waitJS(wChat + `.innerText.includes(` + webJSQ(webDone) + `)`)
	waitUntil(t, "notes.txt written by the accepted card", uiTimeout, func() bool {
		b, _ := os.ReadFile(filepath.Join(r.ws, "notes.txt"))
		return string(b) == webNotes
	})
	tm.WaitGone("Review AI changes", uiTimeout) // answered here, gone there

	// Files: open README.md and read it.
	if vp.Mobile {
		p.goTab("files")
	}
	row := wFiles + `.querySelector('.tree-row[data-path="README.md"]')`
	p.tapJS(row)
	p.waitJS(`location.hash === "#/files/README.md"`)
	if vp.Mobile {
		p.waitJS(wFiles + `.querySelector('.crow[data-i="0"]')?.innerText.includes("# demo")`)
	} else { // the workbench's editor, in its own frame
		p.waitJS(wFiles + `.querySelector(".desk-editor")?.contentDocument?.querySelector(".view-lines")?.textContent.replace(/\u00a0/g, " ").includes("# demo")`)
	}

	// Terminal: open one on the laptop and run a command from the page.
	p.goTab("term")
	open := webButton(wTerm, "Open one")
	line := wTerm + `.querySelector(".term-line")`
	if !vp.Mobile { // the desktop's panel: a click on it, then every key goes to the shell
		line = wTerm + `.querySelector(".dterm-pane.is-focus .term-rows")`
	}
	p.waitJS(open + ` || ` + line)
	if webEval[bool](p, `!!(`+open+`)`) {
		p.tapJS(open)
	}
	p.tapJS(line)
	if !vp.Mobile {
		p.waitJS(`document.activeElement === ` + wTerm + `.querySelector(".dterm-pane.is-focus .dterm-input")`)
	}
	p.keys("echo web-$((6*7))-ok\r")
	p.waitJS(wTerm + `.innerText.includes("web-42-ok")`)

	// Git: stage notes.txt and commit it.
	p.goTab("git")
	if vp.Mobile {
		notes := `document.querySelector('.git-row-wrap[data-path="notes.txt"] .git-row .btn')`
		p.tapJS(notes)
		p.waitJS(`[...document.querySelectorAll(".toasts .toast-title")].some((e) => e.textContent === "Staged notes.txt")`)
		p.tap(".commit-subject")
		p.keys("docs: a line from the web")
		p.tapJS(webButton("document", "Commit 1 file"))
		p.waitJS(`document.activeElement === document.querySelector(".commit-ask button")`)
		p.tapJS(webButton(`document.querySelector(".commit-ask")`, "Commit"))
	} else { // the side bar's source control
		scm := `document.querySelector('.wb-side [data-view="git"]')`
		row := scm + `.querySelector('.tr[data-group="changes"][data-path="notes.txt"]')`
		// The laptop's whole status first (main.go was changed from the
		// start): a list that is still filling in moves the row away from
		// under the pointer between the hover and the click.
		p.waitJS(row + ` && ` + scm + `.querySelector('.tr[data-group="changes"][data-path="main.go"]')`)
		stage := row + `.querySelector('[aria-label="Stage Changes"]')`
		p.hoverTap(row, stage) // its actions show on hover
		p.waitJS(`document.querySelector(".toasts")?.textContent.includes("Staged notes.txt")`)
		p.waitJS(scm + `.querySelector('.tr[data-group="staged"][data-path="notes.txt"]')`) // the laptop's new status
		p.tapJS(scm + `.querySelector("textarea")`)
		p.keys("docs: a line from the web")
		p.waitJS(scm + `.querySelector("textarea").value === "docs: a line from the web"`)
		p.tapJS(scm + `.querySelector('[data-act="commit"]')`)
		p.tapJS(`[...` + scm + `.querySelectorAll(".pv-ask button")].find((b) => b.textContent === "Commit")`)
	}
	p.waitJS(`document.querySelector(".toasts")?.textContent.includes("Committed ")`)
	if got := r.git("log", "-1", "--format=%s"); got != "docs: a line from the web" {
		t.Errorf("last commit %q", got)
	}
	if got := r.git("show", "--name-only", "--format=", "HEAD"); got != "notes.txt" {
		t.Errorf("the commit holds %q, want only notes.txt", got)
	}

	// Stop: the kill switch from the CLI ends the browser session.
	if out, code := r.cli("web", "stop"); code != 0 {
		t.Fatalf("web stop: exit %d: %s", code, out)
	}
	p.waitJS(`document.body.innerText.includes("The web session was stopped.")`)
	waitUntil(t, "web status to say off", uiTimeout, func() bool {
		out, code := r.cli("web", "status")
		return code == 1 && strings.Contains(out, "off")
	})
	tm.Quit()
}
