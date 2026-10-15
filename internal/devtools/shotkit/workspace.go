package shotkit

import (
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// DemoWorkspace copies the files git tracks in src (never untracked files:
// private notes or local builds must not reach a published image) to
// parent/termocode, gives it a small fixed git history (a feature branch
// merged back, a tag; fixed authors and dates) and then makes a few edits,
// so Source Control shows a stable set of changes and a graph. It returns
// the new folder.
func DemoWorkspace(src, parent string) (string, error) {
	dst := filepath.Join(parent, "termocode")
	files, err := trackedFiles(src)
	if err != nil {
		return "", err
	}
	for _, rel := range files {
		p := filepath.Join(src, rel)
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() { // deleted in the checkout, or a link
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, b, 0o644); err != nil {
			return "", err
		}
	}
	if err := demoHistory(dst); err != nil {
		return "", err
	}
	// The Markdown preview scenes open docs/preview.md. It is added after
	// the history and git-excluded, so commit ids and Source Control stay
	// the same.
	if md := filepath.Join(dst, "docs", "preview.md"); !fileExists(md) {
		_ = os.MkdirAll(filepath.Dir(md), 0o755)
		_ = os.WriteFile(md, previewSample, 0o644)
		ex := filepath.Join(dst, ".git", "info", "exclude")
		_ = os.MkdirAll(filepath.Dir(ex), 0o755)
		if f, err := os.OpenFile(ex, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, _ = f.WriteString("/docs/preview.md\n")
			_ = f.Close()
		}
	}
	// A few edits so Source Control has something to show.
	appendTo(dst, "internal/statusbar/model.go", "\n// pill renders a rounded status segment.\n")
	appendTo(dst, "internal/theme/palette.go", "\n// Shadow is the drop-shadow colour.\n")
	appendTo(dst, "README.md", "\nNew screenshots.\n")
	_ = os.WriteFile(filepath.Join(dst, "internal/theme/gradient.go"), []byte("package theme\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(dst, "assets"), 0o755)
	writeDemoImage(filepath.Join(dst, "assets", "demo.png"))
	return dst, nil
}

// previewSample is the Markdown sample for the md-preview scenes.
//
//go:embed testdata/preview.md
var previewSample []byte

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// DemoNow is the wall clock the demo is shown at: a few days after the
// demo history, so ages read "4 days ago" (freeze clock.Now with clock.Set).
var DemoNow = time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)

// demoHistory makes four commits on main (one is a merge of a feature
// branch) with fixed authors and dates, and tags the merge v0.1.0.
func demoHistory(dir string) error {
	var firstErr error
	vcs := func(date string, args ...string) {
		if firstErr != nil {
			return
		}
		cmd := spawn.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=termocode", "GIT_AUTHOR_EMAIL=demo@example.com",
			"GIT_COMMITTER_NAME=termocode", "GIT_COMMITTER_EMAIL=demo@example.com",
			"GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date,
			"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		if out, err := cmd.CombinedOutput(); err != nil {
			firstErr = fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	const d1, d2, d3, d4 = "2026-01-05T09:00:00Z", "2026-01-12T14:30:00Z", "2026-01-20T10:15:00Z", "2026-01-28T17:45:00Z"
	vcs(d1, "init", "-q", "-b", "main")
	vcs(d1, "add", "-A")
	vcs(d1, "commit", "-q", "--no-gpg-sign", "-m", "feat: initial import")
	vcs(d2, "checkout", "-q", "-b", "toast-slide")
	appendTo(dir, "internal/toast/model.go", "\n// slideOnce keeps a leaving toast from sliding twice.\n")
	vcs(d2, "commit", "-q", "--no-gpg-sign", "-am", "feat(toast): slide out once")
	vcs(d3, "checkout", "-q", "main")
	appendTo(dir, "README.md", "\nSee docs/ for the guides.\n")
	vcs(d3, "commit", "-q", "--no-gpg-sign", "-am", "docs: point to the guides")
	vcs(d4, "merge", "-q", "--no-ff", "--no-gpg-sign", "-m", "Merge branch 'toast-slide'", "toast-slide")
	vcs(d4, "tag", "v0.1.0")
	return firstErr
}

// trackedFiles lists the files git tracks in src (relative paths).
func trackedFiles(src string) ([]string, error) {
	cmd := spawn.Command("git", "ls-files", "-z")
	cmd.Dir = src
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files in %s: %w", src, err)
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

func appendTo(root, rel, text string) {
	f, err := os.OpenFile(filepath.Join(root, rel), os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		_, _ = f.WriteString(text)
		_ = f.Close()
	}
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
