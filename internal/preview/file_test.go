package preview

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifyFile(t *testing.T) {
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 3, 4))); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, path string
		data       []byte
		size       int64
		kind, want string
	}{
		{"text", "a.go", []byte("package main\n"), 13, "text", "package main"},
		{"binary", "a.bin", []byte{0, 1, 2}, 12000, "binary", "binary file · 12 KB"},
		{"invalid utf8", "a.txt", []byte{255}, 1, "binary", "binary file"},
		{"image", "a.png", pngData.Bytes(), int64(pngData.Len()), "image", "3 × 4"},
		{"env", ".env.local", []byte("password"), 8, "secret", "hidden: may contain secrets · Enter to open"},
		{"pem", "a.pem", pngData.Bytes(), 5, "secret", "hidden:"},
		{"key", "a.key", []byte("private"), 7, "secret", "hidden:"},
		{"rsa", "id_rsa.pub", []byte("public"), 6, "secret", "hidden:"},
		{"ed25519", "id_ed25519", []byte("private"), 7, "secret", "hidden:"},
		{"ecdsa", "id_ecdsa", []byte("private"), 7, "secret", "hidden:"},
		{"netrc", ".netrc", []byte("password"), 8, "secret", "hidden:"},
		{"p12", "cert.p12", []byte("private"), 7, "secret", "hidden:"},
		{"large", "huge.txt", bytes.Repeat([]byte("a"), FileLimit+10), FileLimit + 10, "text", "preview limited to 64 KB"},
		{"controls", "a.txt", []byte("\x1b[31mhello\a"), 12, "text", "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyFile(tt.path, tt.data, tt.size)
			if got.Kind != tt.kind || !strings.Contains(got.Text, tt.want) {
				t.Fatalf("%+v", got)
			}
			if got.BytesRead > FileLimit {
				t.Fatal("read limit")
			}
			if strings.ContainsAny(got.Text, "\x1b\a") {
				t.Fatal("terminal controls")
			}
			if tt.kind == "secret" && strings.Contains(got.Text, "password") {
				t.Fatal("secret exposed")
			}
		})
	}
	got := ClassifyFile("lines.txt", []byte(strings.Repeat("line\n", 250)), 1250)
	if len(strings.Split(got.Text, "\n")) != 200 {
		t.Fatal("line limit")
	}
}
func TestReadFileBoundedAndFolders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), FileLimit*2), 0600); err != nil {
		t.Fatal(err)
	}
	got := ReadFile(path)
	if got.BytesRead != FileLimit || got.Kind != "text" {
		t.Fatalf("%+v", got)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if got := ReadFile(dir); got.Text != "1 files, 1 folders" {
		t.Fatal(got)
	}
	// Secret names are checked even when the file is absent.
	if got := ReadFile(filepath.Join(dir, ".env")); got.Kind != "secret" || got.BytesRead != 0 {
		t.Fatal(got)
	}
	if err := os.Symlink(path, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if got := ReadFile(filepath.Join(dir, "link")); got.BytesRead != 0 {
		t.Fatal("followed symlink")
	}
	if got := ReadFile(filepath.Join(dir, "missing")); got.Kind != "error" {
		t.Fatal(got)
	}
}
