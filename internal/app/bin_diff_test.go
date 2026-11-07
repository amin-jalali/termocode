package app

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/git"
)

// binTestPNG is a w×h PNG of one colour.
func binTestPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func binTestPDF(text string) []byte {
	content := "BT /F1 12 Tf 72 712 Td (" + text + ") Tj ET"
	return []byte(fmt.Sprintf("%%PDF-1.4\n4 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\ntrailer\n<< >>\n%%%%EOF\n", len(content), content))
}

// A PDF dropped into the chat is sent as its text, not refused.
func TestAIAttachPDFAsText(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "spec.pdf")
	if err := os.WriteFile(p, binTestPDF("Login must take two steps"), 0o644); err != nil {
		t.Fatal(err)
	}
	at, err := aiLoadAttachment(p, root, true)
	if err != nil {
		t.Fatalf("pdf refused: %v", err)
	}
	if at.Label != "spec.pdf" || at.Image != "" || !strings.Contains(at.Text, "Login must take two steps") || !strings.Contains(at.Text, "PDF text") {
		t.Errorf("pdf attachment: %+v", at)
	}
}

// A PDF without any text (a scan) is refused with a clear reason.
func TestAIAttachPDFWithoutText(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "scan.pdf")
	_ = os.WriteFile(p, []byte("%PDF-1.4\ntrailer\n%%EOF\n"), 0o644)
	if _, err := aiLoadAttachment(p, root, true); err == nil || !strings.Contains(err.Error(), "no text") {
		t.Errorf("want a no-text refusal, got %v", err)
	}
}

// Binary images and PDFs in a commit load (Pending) so the card can show
// what changed in them; other binary files keep the binary note.
func TestCommitFilesBinaryImagesLoad(t *testing.T) {
	files := commitFiles(git.CommitInfo{Files: []git.CommitFile{
		{Path: "logo.png", Status: 'M', Binary: true},
		{Path: "spec.pdf", Status: 'A', Binary: true},
		{Path: "blob.bin", Status: 'M', Binary: true},
	}})
	if !files[0].Pending || files[0].Binary {
		t.Errorf("png: %+v", files[0])
	}
	if !files[1].Pending || files[1].Binary {
		t.Errorf("pdf: %+v", files[1])
	}
	if files[2].Pending || !files[2].Binary {
		t.Errorf("bin: %+v", files[2])
	}
}

// binDiffSides turns the two blobs of an image into readable text.
func TestBinDiffSides(t *testing.T) {
	a := string(binTestPNG(t, 4, 4, color.RGBA{1, 2, 3, 255}))
	b := string(binTestPNG(t, 4, 4, color.RGBA{9, 9, 9, 255}))
	o, n, ok := binDiffSides("logo.png", a, b)
	if !ok || !strings.Contains(o, "Size: 4 × 4 px") || !strings.Contains(n, "Changed pixels: 16 of 16") {
		t.Fatalf("ok=%v\n%s\n---\n%s", ok, o, n)
	}
	if o, n, ok := binDiffSides("main.go", "a", "b"); ok || o != "a" || n != "b" {
		t.Errorf("text files pass through: %v %q %q", ok, o, n)
	}
}

// Compare Two Files on two images compares what is in them, not bytes.
func TestCompareTwoImages(t *testing.T) {
	dir := t.TempDir()
	l, r := filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png")
	_ = os.WriteFile(l, binTestPNG(t, 2, 2, color.RGBA{0, 0, 0, 255}), 0o644)
	_ = os.WriteFile(r, binTestPNG(t, 3, 2, color.RGBA{0, 0, 0, 255}), 0o644)
	var m Model
	m.diffLeftPath = l
	msg, ok := m.onDiffRightPicked(r)().(compareReadyMsg)
	if !ok {
		t.Fatalf("want compareReadyMsg")
	}
	if !strings.Contains(msg.file.Old, "Size: 2 × 2 px") || !strings.Contains(msg.file.New, "Size: 3 × 2 px") {
		t.Errorf("compare sides:\n%s\n---\n%s", msg.file.Old, msg.file.New)
	}
}

// The web diff of a binary image has rows that say what changed (and is
// not marked binary, so the sheet shows them).
func TestWebGitBinaryRows(t *testing.T) {
	a := string(binTestPNG(t, 2, 2, color.RGBA{0, 0, 0, 255}))
	b := string(binTestPNG(t, 2, 2, color.RGBA{255, 255, 255, 255}))
	p, ok := webGitBinaryRows("pic.png", a, b)
	if !ok || p.binary {
		t.Fatalf("ok=%v binary=%v (rows must show, so not binary)", ok, p.binary)
	}
	var text strings.Builder
	for _, r := range p.rows {
		text.WriteString(r.K + " " + r.Text + "\n")
	}
	if !strings.Contains(text.String(), "add Changed pixels: 4 of 4") || p.added == 0 {
		t.Errorf("rows:\n%s", text.String())
	}
	if _, ok := webGitBinaryRows("blob.bin", "a", "b"); ok {
		t.Error("blob.bin should not get rows")
	}
}
