package preview

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const FileLimit = 64 * 1024

// FileContent is a bounded, plain text summary. It never contains terminal controls.
type FileContent struct {
	Kind, Text string
	BytesRead  int
}

func SecretPath(path string) bool {
	n := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasPrefix(n, ".env"), n == ".netrc", n == ".pgpass":
		return true
	case strings.HasPrefix(n, "id_rsa"), strings.HasPrefix(n, "id_dsa"), strings.HasPrefix(n, "id_ecdsa"), strings.HasPrefix(n, "id_ed25519"):
		return true
	}
	for _, ext := range []string{".pem", ".key", ".p12", ".pfx"} {
		if strings.HasSuffix(n, ext) {
			return true
		}
	}
	return false
}

// ReadFile reads at most FileLimit bytes. Secret names and special files are
// checked before opening; symlinks are not followed.
func ReadFile(path string) FileContent {
	if SecretPath(path) {
		return FileContent{Kind: "secret", Text: "hidden: may contain secrets · Enter to open"}
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return FileContent{Kind: "error", Text: err.Error()}
	}
	if fi.IsDir() {
		f, err := os.Open(path)
		if err != nil {
			return FileContent{Kind: "error", Text: err.Error()}
		}
		defer f.Close()
		files, dirs := 0, 0
		for {
			entries, err := f.ReadDir(128)
			for _, e := range entries {
				if e.IsDir() {
					dirs++
				} else {
					files++
				}
			}
			if err != nil {
				if err != io.EOF {
					return FileContent{Kind: "error", Text: err.Error()}
				}
				break
			}
		}
		return FileContent{Kind: "folder", Text: fmt.Sprintf("%d files, %d folders", files, dirs)}
	}
	if !fi.Mode().IsRegular() {
		return FileContent{Kind: "binary", Text: "No preview for this file"}
	}
	f, err := os.Open(path)
	if err != nil {
		return FileContent{Kind: "error", Text: err.Error()}
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, FileLimit))
	if err != nil {
		return FileContent{Kind: "error", Text: err.Error()}
	}
	return ClassifyFile(path, b, fi.Size())
}

// ClassifyFile is pure. size is the full size, data is at most FileLimit bytes.
func ClassifyFile(path string, data []byte, size int64) FileContent {
	if SecretPath(path) {
		return FileContent{Kind: "secret", Text: "hidden: may contain secrets · Enter to open"}
	}
	if len(data) > FileLimit {
		data = data[:FileLimit]
	}
	out := FileContent{BytesRead: len(data)}
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		out.Kind = "image"
		out.Text = fmt.Sprintf("%s image · %d × %d · %s", format, cfg.Width, cfg.Height, fileSize(size))
		return out
	}
	// A bounded read may split the final UTF-8 rune.
	text := data
	if size > int64(len(data)) {
		for n := 0; n < 3 && len(text) > 0 && !utf8.Valid(text); n++ {
			text = text[:len(text)-1]
		}
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(text) {
		out.Kind = "binary"
		out.Text = "binary file · " + fileSize(size)
		return out
	}
	out.Kind = "text"
	lines := strings.Split(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n")
	if len(lines) > 200 {
		lines = lines[:200]
	}
	out.Text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return '�'
	}, strings.Join(lines, "\n"))
	out.Text = strings.ReplaceAll(out.Text, "\t", "    ")
	if size > int64(len(data)) {
		out.Text += "\n… preview limited to 64 KB"
	}
	return out
}
func fileSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%d KB", (n+1023)/1024)
}
