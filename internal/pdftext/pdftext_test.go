package pdftext

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
	"testing"
)

// pdf builds a small PDF with one content stream per page.
func pdf(t *testing.T, flate bool, pages ...string) []byte {
	t.Helper()
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	for i, content := range pages {
		data := []byte(content)
		filter := ""
		if flate {
			var z bytes.Buffer
			w := zlib.NewWriter(&z)
			w.Write(data)
			w.Close()
			data, filter = z.Bytes(), " /Filter /FlateDecode"
		}
		fmt.Fprintf(&b, "%d 0 obj\n<< /Length %d%s >>\nstream\n", i+4, len(data), filter)
		b.Write(data)
		b.WriteString("\nendstream\nendobj\n")
	}
	b.WriteString("trailer\n<< >>\n%%EOF\n")
	return b.Bytes()
}

func TestExtractTextOperators(t *testing.T) {
	doc := pdf(t, true,
		"BT /F1 12 Tf 72 712 Td (Hello PDF) Tj 0 -14 Td (Line \\(two\\)) Tj ET",
		"BT [(Spl) -250 (it)] TJ T* <48690a> Tj ET")
	got, err := Extract(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Hello PDF\nLine (two)", "Split", "Hi"} {
		if !strings.Contains(got, want) {
			t.Errorf("text lacks %q:\n%q", want, got)
		}
	}
}

func TestExtractPlainStream(t *testing.T) {
	got, err := Extract(pdf(t, false, "BT (Plain) Tj ET"))
	if err != nil || strings.TrimSpace(got) != "Plain" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestExtractNotAPDF(t *testing.T) {
	if _, err := Extract([]byte("hello")); err == nil {
		t.Error("a text file passed as a PDF")
	}
	if got, err := Extract(pdf(t, true, "0 0 m 10 10 l S")); err != nil || got != "" {
		t.Errorf("drawing only: %q, %v", got, err)
	}
}

func TestExtractBrokenStreamDoesNotFail(t *testing.T) {
	doc := []byte("%PDF-1.4\n1 0 obj\n<< /Filter /FlateDecode >>\nstream\nnot zlib\nendstream\nendobj\n")
	if _, err := Extract(doc); err != nil {
		t.Errorf("broken stream: %v", err)
	}
}

// Any bytes: no panic.
func FuzzExtract(f *testing.F) {
	f.Add([]byte("%PDF-1.4\nstream\nBT (a\\) Tj ET\nendstream"))
	f.Add([]byte("%PDF stream\nBT <4 Tj [( ET endstream"))
	f.Add([]byte("%PDFstream"))
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = Extract(b) })
}
