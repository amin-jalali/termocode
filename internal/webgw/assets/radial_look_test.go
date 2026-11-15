package assets

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/evanw/esbuild/pkg/api"
)

// The radial menu must look like the owner's design (mobocode
// final_blue.png, web/packages/radial-menu/test/final_blue.png): the
// package's design page (demo/design.ts: the design's rings, AI active,
// the "tests" wedge held to a full arc) is screenshot in headless Chrome
// at the design's size and compared with it, then again on a light page,
// where nothing of the page may show through the menu.
func TestRadialMenuLooksLikeTheDesign(t *testing.T) {
	chrome := lookChrome()
	if chrome == "" {
		t.Skip("no Chrome or Chromium: the radial menu's look is not checked here")
	}
	abs, err := filepath.Abs(radialDir)
	if err != nil {
		t.Fatal(err)
	}
	res := api.Build(api.BuildOptions{
		EntryPoints: []string{filepath.Join(abs, "demo", "design.ts")},
		Bundle:      true,
		Write:       false,
		Format:      api.FormatIIFE,
		Platform:    api.PlatformBrowser,
		Target:      api.ES2022,
		LogLevel:    api.LogLevelSilent,
		Outfile:     "design.js",
	})
	if len(res.Errors) > 0 {
		t.Fatalf("bundle the design page:\n%s", strings.Join(api.FormatMessages(res.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage}), ""))
	}
	css, err := os.ReadFile(filepath.Join(abs, "src", "radial-menu.css"))
	if err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(abs, "demo", "design.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := strings.Replace(string(page), `<link rel="stylesheet" href="../src/radial-menu.css">`, "<style>"+string(css)+"</style>", 1)
	html = strings.Replace(html, `<script type="module" src="design.js"></script>`, "<script>"+string(res.OutputFiles[0].Contents)+"</script>", 1)
	if !strings.Contains(html, "<style>") || strings.Contains(html, "design.js") {
		t.Fatal("demo/design.html no longer links the stylesheet and design.js as the test expects")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "design.html")
	if err := os.WriteFile(file, []byte(html), 0o600); err != nil {
		t.Fatal(err)
	}
	dark := chromeShot(t, chrome, "file://"+file, filepath.Join(dir, "dark.png"))
	light := chromeShot(t, chrome, "file://"+file+"#light", filepath.Join(dir, "light.png"))
	ref := readPNG(t, filepath.Join(abs, "test", "final_blue.png"))
	if out := os.Getenv("RADIAL_LOOK_OUT"); out != "" {
		for _, f := range []string{"dark.png", "light.png"} {
			if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil {
				_ = os.WriteFile(filepath.Join(out, "look-"+f), b, 0o600)
			}
		}
	}
	if ref.Bounds().Dx() != 480 || dark.Bounds().Dx() != 480 || dark.Bounds().Dy() != 480 {
		t.Fatalf("sizes: design %v, shot %v", ref.Bounds(), dark.Bounds())
	}

	// The design space maps 600 units to the 480 px box: 0.8 px a unit.
	const k = 0.8
	at := func(r, deg float64) image.Point {
		a := (deg - 90) * math.Pi / 180
		return image.Pt(int(math.Round(240+r*k*math.Cos(a))), int(math.Round(240+r*k*math.Sin(a))))
	}
	// Sampled colours: each within a few levels of the design's.
	samples := []struct {
		name string
		p    image.Point
	}{
		{"the gap between two outer wedges", at(178, 30)},
		{"the gap between two outer wedges (bottom left)", at(178, 210)},
		{"the hairline ring between the rings", at(122, 0)},
		{"between the rings", at(119.5, 90)},
		{"the gap between two categories", at(86, 144)},
		{"an outer wedge", at(192, 60)},
		{"an outer wedge (bottom)", at(150, 200)},
		{"a category wedge", at(100, 108)},
		{"the active category (AI)", at(100, 324)},
		{"the active category's icon (accent blue)", at(86, 324)},
		{"the hold arc", at(207, 120)},
		{"the hub", at(30, 90)},
		{"just past the outer ring", at(222, 45)},
	}
	for _, s := range samples {
		g, w := rgb(dark, s.p), rgb(ref, s.p)
		if d := maxDiff(g, w); d > 12 {
			t.Errorf("%s at %v: %v, the design has %v (off by %d)", s.name, s.p, g, w, d)
		}
	}
	// The light page changes nothing on the disc: the menu is opaque and
	// brings its own palette.
	bad := 0
	discR := 228 * k
	for y := 0; y < 480; y++ {
		for x := 0; x < 480; x++ {
			if math.Hypot(float64(x)-240, float64(y)-240) > discR {
				continue
			}
			if maxDiff(rgb(dark, image.Pt(x, y)), rgb(light, image.Pt(x, y))) > 4 {
				bad++
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d pixels of the menu change on a light page (the page shows through)", bad)
	}
	for _, s := range samples[:5] {
		if c := rgb(light, s.p); lum(c) > 48 {
			t.Errorf("on a light page, %s at %v is %v: not the design's dark", s.name, s.p, c)
		}
	}
	// The whole rings area, pixel by pixel, on both pages.
	for name, img := range map[string]image.Image{"dark page": dark, "light page": light} {
		var sum, n, far float64
		for y := 0; y < 480; y++ {
			for x := 0; x < 480; x++ {
				if math.Hypot(float64(x)-240, float64(y)-240) > 214*k {
					continue
				}
				d := float64(maxDiff(rgb(img, image.Pt(x, y)), rgb(ref, image.Pt(x, y))))
				sum += d
				n++
				if d > 32 {
					far++
				}
			}
		}
		mean := sum / n
		t.Logf("%s: the rings differ from the design by %.2f levels on average, %.2f%% of pixels by more than 32", name, mean, 100*far/n)
		if mean > 3 || far/n > 0.015 {
			t.Errorf("on a %s the rings differ from the design: mean %.2f levels, %.2f%% of pixels off by more than 32", name, mean, 100*far/n)
		}
	}
	// Six outer wedges, as the design draws them, on both pages.
	for name, img := range map[string]image.Image{"the design": ref, "the menu": dark, "the menu on a light page": light} {
		if got := countWedges(img, k); got != 6 {
			t.Errorf("%s has %d outer wedges, want 6", name, got)
		}
	}
}

// countWedges counts the outer ring's wedges by the gaps between them:
// along the circle at r = 195 units (past the icons, inside the hold
// arc's glow) a gap is a short run that differs from the wedge around it
// (the median of ±5°); runs closer than 3° are one gap.
func countWedges(img image.Image, k float64) int {
	const steps = 1440
	l := make([]float64, steps)
	for i := range l {
		a := (float64(i)/4 - 90) * math.Pi / 180
		p := image.Pt(int(math.Round(240+195*k*math.Cos(a))), int(math.Round(240+195*k*math.Sin(a))))
		l[i] = lum(rgb(img, p))
	}
	var idx []int
	win := make([]float64, 41)
	for i := range l {
		for j := range win {
			win[j] = l[(i+j-20+steps)%steps]
		}
		slices.Sort(win)
		if math.Abs(l[i]-win[20]) > 5 {
			idx = append(idx, i)
		}
	}
	n := 0
	for j, i := range idx {
		if j == 0 || i-idx[j-1] >= 12 {
			n++
		}
	}
	if n > 1 && idx[0]+steps-idx[len(idx)-1] < 12 {
		n-- // the run across 0°
	}
	return n
}

func lookChrome() string {
	if p := os.Getenv("CHROME_PATH"); p != "" {
		return p
	}
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}

// chromeShot screenshots url at 480 × 480 in a headless Chrome of its own.
func chromeShot(t *testing.T, chrome, url, out string) image.Image {
	t.Helper()
	args := []string{
		"--headless=new", "--disable-gpu", "--hide-scrollbars", "--no-first-run", "--no-default-browser-check",
		"--disable-background-networking", "--disable-component-update", "--force-color-profile=srgb",
		"--force-device-scale-factor=1", "--window-size=480,480", "--virtual-time-budget=2000",
		"--user-data-dir=" + filepath.Join(t.TempDir(), "profile"), "--screenshot=" + out, url,
	}
	if os.Getuid() == 0 {
		args = append(args, "--no-sandbox")
	}
	cmd := exec.Command(chrome, args...)
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}
	done := make(chan error, 1)
	var b []byte
	go func() {
		var err error
		b, err = cmd.CombinedOutput()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("chrome: %v\n%s", err, b)
		}
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("chrome took over 60 s")
	}
	return readPNG(t, out)
}

func readPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return img
}

type rgb8 [3]int

func (c rgb8) String() string { return fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2]) }

func rgb(img image.Image, p image.Point) rgb8 {
	r, g, b, _ := img.At(p.X, p.Y).RGBA()
	return rgb8{int(r >> 8), int(g >> 8), int(b >> 8)}
}

func maxDiff(a, b rgb8) int {
	m := 0
	for i := range a {
		d := a[i] - b[i]
		if d < 0 {
			d = -d
		}
		m = max(m, d)
	}
	return m
}

func lum(c rgb8) float64 { return 0.2126*float64(c[0]) + 0.7152*float64(c[1]) + 0.0722*float64(c[2]) }
