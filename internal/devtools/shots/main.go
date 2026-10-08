// Command shots renders termocode screens to PNG for the README and for
// visual review. It runs the real app model (with a real embedded nvim) in
// process, drives it with app.DevActionMsg / key messages, grabs the frame
// Bubble Tea would print and rasterises it with internal/devtools/ansipng.
//
//	go run ./internal/devtools/shots -font ~/fonts/JetBrainsMonoNerdFontMono-Regular.ttf -out assets/screenshots
//
// It is a developer tool: it needs nvim on PATH and a Nerd Font file.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/devtools/ansipng"
	"github.com/amin-jalali/termocode/internal/theme"
)

// recorder wraps the app model and keeps the last frame View produced.
type recorder struct {
	inner tea.Model
	mu    *sync.Mutex
	last  *string
}

func (r recorder) Init() tea.Cmd { return r.inner.Init() }

func (r recorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	r.inner, cmd = r.inner.Update(msg)
	return r, cmd
}

func (r recorder) View() string {
	v := r.inner.View()
	r.mu.Lock()
	*r.last = v
	r.mu.Unlock()
	return v
}

type step struct {
	msg  tea.Msg
	wait time.Duration
}

type scene struct {
	name  string
	theme string
	icons string
	env   map[string]string
	steps []step
}

func act(id string) step         { return step{msg: app.DevActionMsg{ID: id}, wait: 400 * time.Millisecond} }
func pause(d time.Duration) step { return step{wait: d} }
func key(s string) step          { return step{msg: keyMsg(s), wait: 300 * time.Millisecond} }

func main() {
	out := flag.String("out", "assets/screenshots", "output directory")
	fontPath := flag.String("font", os.Getenv("TERMOCODE_SHOTS_FONT"), "Nerd Font TTF (regular)")
	boldPath := flag.String("bold", "", "Nerd Font TTF (bold); default: -font with Regular→Bold")
	only := flag.String("only", "", "comma-separated scene names")
	w := flag.Int("w", 150, "columns")
	h := flag.Int("h", 42, "rows")
	ansOnly := flag.Bool("ans", false, "also write the raw .ans frames")
	fallback := flag.String("fallback", strings.Join(defaultFallbacks(), ","), "comma-separated fallback fonts for missing glyphs")
	flag.Parse()
	if err := enterDemoWorkspace(); err != nil {
		fmt.Fprintln(os.Stderr, "shots:", err)
		os.Exit(1)
	}
	if *fontPath == "" {
		fmt.Fprintln(os.Stderr, "shots: -font is required (a Nerd Font .ttf)")
		os.Exit(2)
	}
	if *boldPath == "" {
		*boldPath = strings.Replace(*fontPath, "Regular", "Bold", 1)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	absOut, _ := filepath.Abs(*out)

	editorSteps := []step{
		act("open:internal/toast/model.go"),
		act("open:internal/theme/palette.go"),
		act("open:internal/statusbar/model.go"),
		pause(900 * time.Millisecond),
	}
	scenes := []scene{
		{name: "dashboard", steps: []step{pause(1500 * time.Millisecond)}},
		{name: "editor", steps: editorSteps},
		{name: "palette", steps: append(append([]step{}, editorSteps...), key("f1"), key("t"), key("h"), key("e"), key("m"), pause(300*time.Millisecond))},
		{name: "toast", steps: append(append([]step{}, editorSteps...), act("toast:Saved|internal/statusbar/model.go · 480 lines"), pause(200*time.Millisecond))},
		{name: "scm", steps: append(append([]step{}, editorSteps...), act("view:git"), pause(1500*time.Millisecond))},
	}
	with := func(extra ...step) []step { return append(append([]step{}, editorSteps...), extra...) }
	scenes = append(scenes,
		scene{name: "confirm", steps: with(act("palette:git-discard"), pause(400*time.Millisecond))},
		scene{name: "ai-chat", env: map[string]string{"TERMOCODE_AI_PROVIDER": "ollama", "TERMOCODE_AI_MODEL": "qwen2.5-coder"},
			steps: with(act("palette:ai-open-chat"), pause(600*time.Millisecond))},
		scene{name: "problems", steps: with(act("palette:problems-panel"), pause(800*time.Millisecond))},
		scene{name: "testing", steps: with(act("palette:testing-focus"), pause(1500*time.Millisecond))},
		scene{name: "run-debug", steps: with(act("palette:debug-view"), pause(800*time.Millisecond))},
		scene{name: "terminal", steps: with(act("palette:toggle-terminal"), pause(1500*time.Millisecond),
			act("termsend:ls --color=always -F"), pause(1200*time.Millisecond))},
		scene{name: "image-preview", steps: with(act("openfile:assets/demo.png"), pause(800*time.Millisecond))},
		scene{name: "quick-open", steps: with(key("ctrl+p"), key("m"), key("o"), key("d"), pause(800*time.Millisecond))},
	)
	for _, th := range []string{"catppuccin-mocha", "catppuccin-latte", "tokyo-night", "rose-pine", "rose-pine-dawn"} {
		scenes = append(scenes, scene{name: "theme-" + th, theme: th, steps: editorSteps})
		scenes = append(scenes, scene{name: "theme-" + th + "-palette", theme: th,
			steps: with(key("f1"), key("t"), key("h"), key("e"), key("m"), pause(300*time.Millisecond))})
	}
	scenes = append(scenes,
		scene{name: "theme-catppuccin-latte-dashboard", theme: "catppuccin-latte", steps: []step{pause(1500 * time.Millisecond)}},
		scene{name: "theme-catppuccin-latte-scm", theme: "catppuccin-latte", steps: with(act("view:git"), pause(1500*time.Millisecond))},
	)
	for _, mode := range []string{"unicode", "ascii"} {
		scenes = append(scenes, scene{name: "icons-" + mode, icons: mode, steps: editorSteps})
	}

	want := map[string]bool{}
	for _, s := range strings.Split(*only, ",") {
		if s != "" {
			want[s] = true
		}
	}
	for _, sc := range scenes {
		if len(want) > 0 && !want[sc.name] {
			continue
		}
		frame, err := runScene(sc, *w, *h)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", sc.name, err)
			continue
		}
		bg := theme.Hex(theme.BgEditor)
		r, err := ansipng.New(ansipng.Options{
			FontRegular: *fontPath, FontBold: *boldPath, Size: 15,
			Fallbacks: splitList(*fallback),
			DefaultBG: hexRGBA(bg), DefaultFG: hexRGBA(theme.Hex(theme.TextPrimary)),
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		path := filepath.Join(absOut, sc.name+".png")
		if err := r.WritePNG(path, frame, *w); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if *ansOnly {
			_ = os.WriteFile(filepath.Join(absOut, sc.name+".ans"), []byte(frame), 0o644)
		}
		fmt.Println("wrote", path)
	}
}

// runScene starts a fresh app in a private config dir, plays the steps and
// returns the last frame.
func runScene(sc scene, w, h int) (string, error) {
	cfg, err := os.MkdirTemp("", "termocode-shots-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(cfg)
	_ = os.MkdirAll(filepath.Join(cfg, "termocode"), 0o755)
	th := sc.theme
	if th == "" {
		th = "vscode-dark-plus"
	}
	icons := sc.icons
	if icons == "" {
		icons = "nerd"
	}
	conf := fmt.Sprintf(`{"theme": %q, "icons": %q, "animations": "off", "images": "blocks"}`, th, icons)
	_ = os.WriteFile(filepath.Join(cfg, "termocode", "config.json"), []byte(conf), 0o644)
	os.Setenv("XDG_CONFIG_HOME", cfg)
	if home, err := os.UserHomeDir(); err == nil && os.Getenv("TERMOCODE_PLUGINS_DIR") == "" {
		if p := filepath.Join(home, ".local", "share", "termocode", "plugins"); dirExists(p) {
			os.Setenv("TERMOCODE_PLUGINS_DIR", p)
		}
	}
	for k, v := range sc.env {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	switch icons {
	case "unicode":
		theme.CurrentIconMode = theme.IconModeUnicode
	case "ascii":
		theme.CurrentIconMode = theme.IconModeASCII
	default:
		theme.CurrentIconMode = theme.IconModeNerdFont
	}

	var mu sync.Mutex
	last := ""
	rec := recorder{inner: app.New(), mu: &mu, last: &last}
	p := tea.NewProgram(rec, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutSignalHandler())
	done := make(chan struct{})
	go func() {
		_, _ = p.Run()
		close(done)
	}()
	p.Send(tea.WindowSizeMsg{Width: w, Height: h})
	time.Sleep(1500 * time.Millisecond)
	for _, st := range sc.steps {
		if st.msg != nil {
			p.Send(st.msg)
		}
		time.Sleep(st.wait)
	}
	// One more resize forces a fresh View with the final state.
	p.Send(tea.WindowSizeMsg{Width: w, Height: h})
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	frame := last
	mu.Unlock()
	p.Kill()
	<-done
	return frame, nil
}

func hexRGBA(h string) color.RGBA {
	var r, g, b uint8
	if _, err := fmt.Sscanf(strings.TrimPrefix(h, "#"), "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.RGBA{0x1c, 0x1c, 0x1c, 0xff}
	}
	return color.RGBA{r, g, b, 0xff}
}

// enterDemoWorkspace copies the repository (without .git) into a temp
// folder named "termocode", commits it in a fresh git repo and then makes a
// few edits, so every shot shows a clean project name, branch "main" and a
// small, stable set of changes in Source Control.
func enterDemoWorkspace() error {
	repo, err := os.Getwd()
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "termocode-demo-")
	if err != nil {
		return err
	}
	dst := filepath.Join(tmp, "termocode")
	err = filepath.WalkDir(repo, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repo, p)
		switch d.Name() {
		case ".git", "dist", ".venv", "site":
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		return err
	}
	vcs := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dst
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=termocode", "GIT_AUTHOR_EMAIL=demo@example.com",
			"GIT_COMMITTER_NAME=termocode", "GIT_COMMITTER_EMAIL=demo@example.com")
		_ = cmd.Run()
	}
	vcs("init", "-q", "-b", "main")
	vcs("add", "-A")
	vcs("commit", "-q", "-m", "feat: initial import")
	// A few edits so Source Control has something to show.
	appendTo := func(rel, text string) {
		f, err := os.OpenFile(filepath.Join(dst, rel), os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(text)
			_ = f.Close()
		}
	}
	appendTo("internal/statusbar/model.go", "\n// pill renders a rounded status segment.\n")
	appendTo("internal/theme/palette.go", "\n// Shadow is the drop-shadow colour.\n")
	appendTo("README.md", "\nNew screenshots.\n")
	_ = os.WriteFile(filepath.Join(dst, "internal/theme/gradient.go"), []byte("package theme\n"), 0o644)
	writeDemoImage(filepath.Join(dst, "assets", "demo.png"))
	if err := os.Chdir(dst); err != nil {
		return err
	}
	return os.Setenv("PWD", dst)
}

// writeDemoImage draws a small synthetic landscape (sky gradient, sun,
// hills) for the image-preview shot.
func writeDemoImage(path string) {
	const w, h = 480, 270
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		t := float64(y) / h
		for x := 0; x < w; x++ {
			c := color.RGBA{uint8(40 + 180*t), uint8(60 + 90*t), uint8(140 + 60*(1-t)), 255}
			dx, dy := float64(x-340), float64(y-95)
			if dx*dx+dy*dy < 38*38 {
				c = color.RGBA{255, 214, 120, 255}
			}
			hill1 := 190 + 25*math.Sin(float64(x)/55)
			hill2 := 220 + 18*math.Sin(float64(x)/35+1)
			if float64(y) > hill1 {
				c = color.RGBA{60, 110, 90, 255}
			}
			if float64(y) > hill2 {
				c = color.RGBA{35, 75, 70, 255}
			}
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	_ = png.Encode(f, img)
}

func defaultFallbacks() []string {
	var out []string
	for _, p := range []string{
		"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
		"/usr/share/fonts/truetype/noto/NotoSansSymbols2-Regular.ttf",
		"/usr/share/fonts/truetype/noto/NotoSansSymbols-Regular.ttf",
		"/usr/share/fonts/truetype/noto/NotoSansMath-Regular.ttf",
	} {
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// keyMsg builds the tea.KeyMsg whose String() is s ("ctrl+p", "alt+x", "a").
func keyMsg(s string) tea.KeyMsg {
	for k := tea.KeyType(-200); k < 200; k++ {
		if k == tea.KeyRunes {
			continue
		}
		if (tea.KeyMsg{Type: k}).String() == s {
			return tea.KeyMsg{Type: k}
		}
	}
	if strings.HasPrefix(s, "alt+") {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s[4:]), Alt: true}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
