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
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/devtools/ansipng"
	"github.com/amin-jalali/termocode/internal/devtools/shotkit"
	"github.com/amin-jalali/termocode/internal/theme"
)

func main() {
	out := flag.String("out", "assets/screenshots", "output directory")
	fontPath := flag.String("font", os.Getenv("TERMOCODE_SHOTS_FONT"), "Nerd Font TTF (regular)")
	boldPath := flag.String("bold", "", "Nerd Font TTF (bold); default: -font with Regular→Bold")
	only := flag.String("only", "", "comma-separated scene names")
	w := flag.Int("w", 150, "columns")
	h := flag.Int("h", 42, "rows")
	ansOnly := flag.Bool("ans", false, "also write the raw .ans frames")
	fallback := flag.String("fallback", strings.Join(defaultFallbacks(), ","), "comma-separated fallback fonts for missing glyphs")
	real := flag.Bool("real", false, "use the current repo as is (no demo workspace)")
	flag.Parse()
	// Resolve -out before entering the demo workspace (it changes directory).
	absOut, _ := filepath.Abs(*out)
	if *real {
		// keep the working directory
	} else if err := enterDemoWorkspace(); err != nil {
		fmt.Fprintln(os.Stderr, "shots:", err)
		os.Exit(1)
	} else {
		// The demo history has fixed dates; show it a few days later.
		clock.Set(func() time.Time { return shotkit.DemoNow })
	}
	if *fontPath == "" {
		fmt.Fprintln(os.Stderr, "shots: -font is required (a Nerd Font .ttf)")
		os.Exit(2)
	}
	if *boldPath == "" {
		*boldPath = strings.Replace(*fontPath, "Regular", "Bold", 1)
	}
	if err := os.MkdirAll(absOut, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	scenes := shotkit.Scenes()

	want := map[string]bool{}
	for _, s := range strings.Split(*only, ",") {
		if s != "" {
			want[s] = true
		}
	}
	for _, sc := range scenes {
		if len(want) > 0 && !want[sc.Name] {
			continue
		}
		frame, err := shotkit.Run(sc, *w, *h, shotkit.Options{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", sc.Name, err)
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
		path := filepath.Join(absOut, sc.Name+".png")
		if err := r.WritePNG(path, frame, *w); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if *ansOnly {
			_ = os.WriteFile(filepath.Join(absOut, sc.Name+".ans"), []byte(frame), 0o644)
		}
		fmt.Println("wrote", path)
	}
}

func hexRGBA(h string) color.RGBA {
	var r, g, b uint8
	if _, err := fmt.Sscanf(strings.TrimPrefix(h, "#"), "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.RGBA{0x1c, 0x1c, 0x1c, 0xff}
	}
	return color.RGBA{r, g, b, 0xff}
}

// enterDemoWorkspace copies the repository into a temp folder named
// "termocode" with a small fixed git history and a few edits (see
// shotkit.DemoWorkspace) and makes it the working directory.
func enterDemoWorkspace() error {
	repo, err := os.Getwd()
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "termocode-demo-")
	if err != nil {
		return err
	}
	dst, err := shotkit.DemoWorkspace(repo, tmp)
	if err != nil {
		return err
	}
	if err := os.Chdir(dst); err != nil {
		return err
	}
	return os.Setenv("PWD", dst)
}

func defaultFallbacks() []string {
	var out []string
	for _, p := range []string{
		"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", // Arabic presentation forms (RTL chat)
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
