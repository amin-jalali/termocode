package shotkit

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DemoWorkspace copies src (without .git and build output) to
// parent/termocode, gives it a small fixed git history (a feature branch
// merged back, a tag; fixed authors and dates) and then makes a few edits,
// so Source Control shows a stable set of changes and a graph. It returns
// the new folder.
func DemoWorkspace(src, parent string) (string, error) {
	dst := filepath.Join(parent, "termocode")
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
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
		return "", err
	}
	if err := demoHistory(dst); err != nil {
		return "", err
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

// demoHistory makes four commits on main (one is a merge of a feature
// branch) with fixed authors and dates, and tags the merge v0.1.0.
func demoHistory(dir string) error {
	var firstErr error
	vcs := func(date string, args ...string) {
		if firstErr != nil {
			return
		}
		cmd := exec.Command("git", args...)
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
