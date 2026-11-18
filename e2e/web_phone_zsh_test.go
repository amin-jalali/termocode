//go:build e2e && browser

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// The owner's iPhone (2026-11-15): on the phone's Term page with the
// Tactile keyboard and zsh with an oh-my-zsh robbyrussell prompt, "every
// character I type, it presses Enter": prompt lines piled up while he
// typed. zsh got the right bytes; the phone drew them wrong. Each key
// makes zsh redraw its line, the page gets new rows and the terminal
// engine redraws from the first changed line. The engine parses what it
// is given later (its writes are queued), so a second redraw that came
// before the first was parsed moved the cursor by stale rows and drew
// the prompt and the output again below the old ones. A phone's slower
// CPU makes that common; the test slows Chrome's CPU like a phone's.
//
// The real binary, nvim and zsh with that prompt; the phone types
// `ls -lah` key by key on the Tactile keyboard and Enter: zsh runs it
// once and draws one prompt, and the phone shows each line once.

// zshrcRobby is a robbyrussell-like prompt (the theme's own lines, with
// its git_prompt_info written here) plus what oh-my-zsh adds on each
// prompt: the window title. precmd and preexec log each prompt and each
// command zsh runs.
const zshrcRobby = `autoload -U colors && colors
setopt PROMPT_SUBST
git_prompt_info() {
  local b
  b=$(git symbolic-ref --short HEAD 2>/dev/null) || return 0
  if [[ -n $(git status --porcelain 2>/dev/null | head -1) ]]; then
    print -rn -- "%{$fg_bold[blue]%}git:(%{$fg[red]%}$b%{$fg[blue]%}) %{$fg[yellow]%}%1{✗%}%{$reset_color%} "
  else
    print -rn -- "%{$fg_bold[blue]%}git:(%{$fg[red]%}$b%{$fg[blue]%})%{$reset_color%} "
  fi
}
PROMPT="%(?:%{$fg_bold[green]%}%1{➜%} :%{$fg_bold[red]%}%1{➜%} ) %{$fg[cyan]%}%c%{$reset_color%}"
PROMPT+=' $(git_prompt_info)'
precmd() { print -Pn "\e]2;%n@%m: %~\a"; print -r -- P >> "$HOME/prompts.log" }
preexec() { print -Pn "\e]2;$1\a"; print -r -- "$1" >> "$HOME/cmds.log" }
`

// webPhoneTall is a tall phone (430×932, the owner's).
var webPhoneTall = webViewport{Name: "phone", W: 430, H: 932, Scale: 3, Mobile: true}

func TestWebRealPhoneZshTactileTypesOneLine(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	r := newWebRig(t)
	writeFile(t, filepath.Join(r.home, ".zshrc"), zshrcRobby)
	r.env = append(r.env, "SHELL="+zsh, "ZDOTDIR="+r.home)
	tm := Start(t, StartOpts{Cols: 140, Rows: 40, Home: r.home, Workspace: r.ws, Env: r.env})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	link := r.startPanel()
	tm.WaitText("Web panel is on", uiTimeout)
	p := newWebPage(t, webPhoneTall)
	p.do(chromedp.Navigate(link))
	p.tap(".connect-foot button")
	p.waitJS(`document.documentElement.dataset.link === "online"`)
	p.goTab("term")
	open := webButton(wTerm, "Open one")
	x := wTerm + `.querySelector(".pterm-x[data-ready]")`
	p.waitJS(open + ` || ` + x)
	if webEval[bool](p, `!!(`+open+`)`) {
		p.tapJS(open)
	}
	p.waitJS(x)
	prompts := func() int {
		b, _ := os.ReadFile(filepath.Join(r.home, "prompts.log"))
		return strings.Count(string(b), "P\n")
	}
	waitUntil(t, "zsh's first prompt", uiTimeout, func() bool { return prompts() >= 1 })
	p.waitJS(wTerm + `.innerText.includes("➜")`)
	p.tapJS(x + `.querySelector(".xterm-screen")`)
	dock := `document.querySelector(".tk-dock")`
	p.waitJS(dock + ` && !` + dock + `.hidden`)
	// A phone's CPU: the engine parses its writes later.
	p.do(chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		_, err := cdp.Call(ctx, tg, emulation.SetCPUThrottlingRate, emulation.SetCPUThrottlingRateParams{Rate: 20})
		return err
	}))
	key := func(id string) string {
		return `document.querySelector('.tk-dock:not([hidden]) .tk-key[data-key=` + webJSQ(id) + `]')`
	}
	// rows are the phone terminal's lines, without the empty ones.
	rows := func() []string {
		var out []string
		for _, l := range strings.Split(webEval[string](p, x+`.querySelector(".xterm-rows").innerText`), "\n") {
			if l = strings.TrimRight(strings.ReplaceAll(l, " ", " "), " "); l != "" {
				out = append(out, l)
			}
		}
		return out
	}
	// count counts s in the rows read as one text: a long `ls -lah` row
	// wraps on the phone (macOS's ls writes "34B" and "staff", so
	// "README.md" broke over two rows there), and a wrapped name is still
	// there once.
	count := func(ls []string, s string) int {
		return strings.Count(strings.Join(ls, ""), s)
	}
	// worst watches every frame the phone draws: the most times a text
	// was on the screen at once.
	webEval[bool](p, `(() => {
		const W = window.__worst = {};
		const rows = `+x+`.querySelector(".xterm-rows");
		const look = () => {
			const t = rows.innerText.replace(/\n/g, ""); // a wrapped row is one text
			for (const s of ["➜", "ls -lah", "total ", "README.md"]) {
				const n = t.split(s).length - 1;
				if (n > (W[s] ?? 0)) (W[s] = n), (W[s + " frame"] = t);
			}
		};
		new MutationObserver(look).observe(rows, { subtree: true, childList: true, characterData: true });
		return true;
	})()`)
	frame := func(s string) string { return webEval[string](p, `window.__worst[`+webJSQ(s+" frame")+`] ?? ""`) }
	worst := func(s string) int { return int(webEval[float64](p, `window.__worst[`+webJSQ(s)+`] ?? 0`)) }
	start := prompts()
	for _, k := range []string{"L", "S", "space", "-", "L", "A", "H"} {
		p.tapJS(key(k))
		time.Sleep(150 * time.Millisecond) // a person's pace: zsh redraws its line after each key
	}
	p.waitJS(x + `.querySelector(".xterm-rows").innerText.includes("ls -lah")`)
	time.Sleep(time.Second) // let late redraws land
	if n := prompts() - start; n != 0 {
		t.Errorf("zsh drew %d new prompts while `ls -lah` was typed, want none (no Enter yet)", n)
	}
	if got := rows(); count(got, "➜") != 1 || count(got, "ls -lah") != 1 {
		t.Errorf("while typing, the phone shows %d prompts and `ls -lah` %d times, want 1 and 1:\n%s",
			count(got, "➜"), count(got, "ls -lah"), strings.Join(got, "\n"))
	}
	if w := worst("➜"); w != 1 {
		t.Errorf("while typing, a frame on the phone showed %d prompts, want 1:\n%s", w, frame("➜"))
	}
	p.tapJS(key("enter"))
	waitUntil(t, "ls -lah to run", uiTimeout, func() bool { return prompts() > start })
	p.waitJS(x + `.querySelector(".xterm-rows").innerText.replace(/\n/g, "").includes("README.md")`)
	time.Sleep(time.Second)
	cmds, _ := os.ReadFile(filepath.Join(r.home, "cmds.log"))
	if string(cmds) != "ls -lah\n" {
		t.Errorf("zsh ran %q, want \"ls -lah\" once", cmds)
	}
	if n := prompts() - start; n != 1 {
		t.Errorf("zsh drew %d prompts after one Enter, want 1", n)
	}
	got := rows()
	if count(got, "➜") != 2 || count(got, "ls -lah") != 1 || count(got, "total ") != 1 || count(got, "README.md") != 1 {
		t.Errorf("after Enter the phone shows %d prompts, `ls -lah` %d times, `total` %d times, README.md %d times; want 2, 1, 1, 1:\n%s",
			count(got, "➜"), count(got, "ls -lah"), count(got, "total "), count(got, "README.md"), strings.Join(got, "\n"))
	}
	for _, s := range []string{"ls -lah", "total ", "README.md"} {
		if w := worst(s); w != 1 {
			t.Errorf("a frame on the phone showed %q %d times, want once:\n%s", s, w, frame(s))
		}
	}
	if w := worst("➜"); w != 2 {
		t.Errorf("a frame on the phone showed %d prompts, want 2 at most:\n%s", w, frame("➜"))
	}
	// The lines in their order: the new prompt is the last, under the output.
	if last := len(got) - 1; last < 1 || !strings.Contains(got[last], "➜") || !strings.Contains(got[last-1], "pkg") {
		t.Errorf("the new prompt is not the last line, under the output:\n%s", strings.Join(got, "\n"))
	}
	tm.Quit()
}
