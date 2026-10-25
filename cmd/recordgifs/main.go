// Command recordgifs regenerates the presentation GIFs in assets/gifs.
//
//	go run ./cmd/recordgifs                     # all GIFs
//	go run ./cmd/recordgifs -only db,secrets    # some
//
// It is a developer tool: it needs nvim, git and a Nerd Font .ttf. Seven
// GIFs play shotkit scenes (the real app model in process, no terminal);
// hero.gif runs the real binary in a PTY through the e2e "record" test
// (a real disconnect and reattach of the persistent session). See
// assets/gifs/README.md.
package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/devtools/ansipng"
	"github.com/amin-jalali/termocode/internal/devtools/gifkit"
)

func main() { os.Exit(run()) }

func run() int {
	out := flag.String("out", "assets/gifs", "output directory")
	font := flag.String("font", defaultFont(), "Nerd Font TTF (regular)")
	bold := flag.String("bold", "", "Nerd Font TTF (bold); default: -font with Regular→Bold")
	only := flag.String("only", "", "comma-separated GIF names (default: all)")
	frames := flag.String("frames", "", "also write every frame of each GIF as PNG into this directory (for review)")
	flag.Parse()
	if *font == "" {
		fmt.Fprintln(os.Stderr, "recordgifs: -font is required (a Nerd Font .ttf, or set TERMOCODE_SHOTS_FONT)")
		return 2
	}
	if *bold == "" {
		*bold = strings.Replace(*font, "Regular", "Bold", 1)
	}
	repo, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// The app keeps backups, AI run state and caches under the XDG state,
	// data and cache dirs: point them at a temp dir, so a recording never
	// writes into (or reads) the real ones.
	// TMPDIR too: the Go tools that run inside write telemetry into the
	// private config dirs after a scene removed them, and everything left
	// over goes away with the sandbox. (The name is short: unix socket
	// paths of the persistent session are limited to about 100 bytes.)
	sandbox, err := os.MkdirTemp("", "tc-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(sandbox)
	if err := os.MkdirAll(filepath.Join(sandbox, "t"), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	os.Setenv("TMPDIR", filepath.Join(sandbox, "t"))
	for k, d := range map[string]string{"XDG_STATE_HOME": "state", "XDG_DATA_HOME": "data", "XDG_CACHE_HOME": "cache"} {
		os.Setenv(k, filepath.Join(sandbox, d))
	}
	absOut, _ := filepath.Abs(*out)
	r, err := gifkit.Style{Font: *font, Bold: *bold, Fallbacks: gifkit.DefaultFallbacks()}.Renderer()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	want := map[string]bool{}
	for _, n := range strings.Split(*only, ",") {
		if n != "" {
			want[n] = true
		}
	}
	emit := func(c gifkit.Clip) error {
		n, err := gifkit.Write(c, r, absOut)
		if err != nil {
			return err
		}
		fmt.Printf("wrote %s/%s.gif  %.1f s  %.2f MB\n", *out, c.Name, c.Duration().Seconds(), float64(n)/(1<<20))
		if *frames != "" {
			return dumpFrames(c, r, filepath.Join(*frames, c.Name))
		}
		return nil
	}
	code := 0
	// The hero is the real binary in a PTY: the e2e record test.
	if len(want) == 0 || want["hero"] {
		if err := recordHero(repo, emit); err != nil {
			fmt.Fprintln(os.Stderr, "recordgifs: hero:", err)
			code = 1
		}
	}
	cleanup, err := gifkit.EnterDemoWorkspace()
	if err != nil {
		fmt.Fprintln(os.Stderr, "recordgifs:", err)
		return 1
	}
	defer cleanup()
	for _, sc := range gifkit.SceneClips(repo) {
		if len(want) > 0 && !want[sc.Name] {
			continue
		}
		if sc.ConfigDir != "" {
			defer os.RemoveAll(sc.ConfigDir) // a scene that shows its config path
		}
		c, err := gifkit.Record(sc)
		if err == nil {
			err = emit(c)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "recordgifs:", err)
			code = 1
		}
	}
	return code
}

// recordHero runs the e2e record test, which drives the real binary and
// writes its frames, then adds the title frame and encodes.
func recordHero(repo string, emit func(gifkit.Clip) error) error {
	dir, err := os.MkdirTemp("", "termocode-hero-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	cmd := exec.Command("go", "-C", filepath.Join(repo, "e2e"), "test", "-tags", "e2e,record",
		"-run", "^TestRecordHero$", "-count=1", "-timeout", "5m", ".")
	cmd.Env = append(os.Environ(), "GIF_FRAMES_DIR="+dir)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	c, err := gifkit.Load(dir)
	if err != nil {
		return err
	}
	return emit(gifkit.WithTitle(c))
}

func dumpFrames(c gifkit.Clip, r *ansipng.Renderer, dir string) error {
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i, f := range c.Frames {
		p, err := os.Create(filepath.Join(dir, fmt.Sprintf("%03d.png", i)))
		if err != nil {
			return err
		}
		err = png.Encode(p, r.Render(gifkit.Normalize(f.ANSI), gifkit.Cols))
		p.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func defaultFont() string {
	if f := os.Getenv("TERMOCODE_SHOTS_FONT"); f != "" {
		return f
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(home, ".local/share/fonts/JetBrainsMonoNF/JetBrainsMonoNerdFontMono-Regular.ttf"),
		filepath.Join(home, "fonts/JetBrainsMonoNerdFontMono-Regular.ttf"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
