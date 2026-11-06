package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
)

// Spec (FIX-3): the image preview of a container workspace reads the
// picture through the Host FS, never the local disk.
func TestRCImagePreviewReadsHost(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	ws, h := newRCWorkspace(t, fstest.MapFS{"pic.png": {Data: buf.Bytes()}})
	m := Model{ws: ws}
	p := filepath.Join(ws.Root, "pic.png")
	msg, ok := m.openImagePreview(p)().(imageLoadedMsg)
	if !ok {
		t.Fatal("not an imageLoadedMsg")
	}
	if msg.err != nil {
		t.Fatalf("container image not shown: %v", msg.err)
	}
	if b := msg.img.Bounds(); b.Dx() != 3 || b.Dy() != 2 {
		t.Errorf("image %v, want 3x2", b)
	}
	if msg.size != int64(buf.Len()) {
		t.Errorf("size %d, want %d", msg.size, buf.Len())
	}
	if !slices.Contains(h.calls(), "read "+p) {
		t.Errorf("not read through the Host: %q", h.calls())
	}

	// The local decoy is not a picture: the container has no such file.
	if msg := m.openImagePreview(filepath.Join(ws.Root, "local.png"))().(imageLoadedMsg); msg.err == nil {
		t.Error("local.png came from the local disk")
	}
}
