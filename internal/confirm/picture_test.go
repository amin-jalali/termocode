package confirm

import (
	"regexp"
	"strings"
	"testing"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// WithPicture shows lines exactly as given (a QR code): every space kept,
// no wrap, no "more lines" cut however tall, the card wide enough, and
// the buttons still under it.
func TestWithPictureVerbatim(t *testing.T) {
	var pic []string
	for i := 0; i < 25; i++ {
		pic = append(pic, "█ ▀▄  █"+strings.Repeat("▄ ", 20)+"█")
	}
	m := New("Scan", "Scan the code.", []Button{{ID: "close", Title: "Close"}}).WithPicture(pic)
	m.SetSize(120, 60)
	plain := ansiRe.ReplaceAllString(m.View(), "")
	if strings.Contains(plain, "more lines") {
		t.Fatal("the picture was cut")
	}
	if n := strings.Count(plain, "█ ▀▄  █"); n != 25 {
		t.Fatalf("%d picture rows kept verbatim, want 25\n%s", n, plain)
	}
	if strings.Index(plain, "Close") < strings.LastIndex(plain, "█ ▀▄  █") {
		t.Fatal("the button is not under the picture")
	}
}
