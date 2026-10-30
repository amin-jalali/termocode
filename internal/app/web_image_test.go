package app

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

var (
	webPNG  = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"
	webJPEG = "\xff\xd8\xff\xe0\x00\x10JFIF"
	webGIF  = "GIF89a\x01\x00\x01\x00"
	webWEBP = "RIFF\x10\x00\x00\x00WEBPVP8 "
)

// Spec (WEB-57): file.image sends a workspace image as bytes with its
// type read from its first bytes; anything else (SVG, text named .png),
// a file over the cap, a path outside and a secret-looking name are
// refused.
func TestFileImage(t *testing.T) {
	r := newWebFilesRig(t)
	for name, body := range map[string]string{"a.png": webPNG, "b.jpg": webJPEG, "c.gif": webGIF, "d.webp": webWEBP} {
		r.write(name, body)
		var res webproto.ImageRes
		webResData(t, r.call(webproto.OpFileImage, &webproto.PathArgs{Path: name}), &res)
		got, _ := base64.StdEncoding.DecodeString(res.Data)
		if string(got) != body || res.Size != len(body) || !strings.HasPrefix(res.Mime, "image/") {
			t.Errorf("%s: %+v", name, res)
		}
	}
	r.write("e.svg", `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	r.write("f.png", "just text")
	r.write("g.png", webPNG+strings.Repeat("x", webproto.MaxImageBytes))
	r.write(".env.png", webPNG)
	for path, code := range map[string]string{"e.svg": "bad_request", "f.png": "bad_request", "g.png": "limit", "nope.png": "not_found", ".env.png": "forbidden"} {
		if f := r.call(webproto.OpFileImage, &webproto.PathArgs{Path: path}); webErrCode(f) != code {
			t.Errorf("%s: %v, want %s", path, f, code)
		}
	}
	out := filepath.Join(t.TempDir(), "o.png")
	if err := os.WriteFile(out, []byte(webPNG), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(r.root, "link.png")); err != nil {
		t.Fatal(err)
	}
	if f := r.call(webproto.OpFileImage, &webproto.PathArgs{Path: "link.png"}); f["t"] != "err" {
		t.Errorf("a link out of the workspace: %v", f)
	}
}

// Spec (WEB-57): chat.image sends an image the user attached to a
// message of the laptop's chat; a message or image that is not there is
// not_found; a file that is no image any more is refused.
func TestChatImage(t *testing.T) {
	r := newWebFilesRig(t)
	img := r.write("shot.png", webPNG)
	txt := r.write("t.png", "text now")
	s := r.m.aiS()
	s.conv.Messages = append(s.conv.Messages, ai.StoredMessage{Role: ai.StoredUser, Text: "look", Images: []string{img, txt}})
	i := len(s.conv.Messages) - 1
	var res webproto.ImageRes
	webResData(t, r.call(webproto.OpChatImage, &webproto.ChatImageArgs{I: i, N: 0}), &res)
	if res.Mime != "image/png" || res.Size != len(webPNG) {
		t.Errorf("chat image: %+v", res)
	}
	if f := r.call(webproto.OpChatImage, &webproto.ChatImageArgs{I: i, N: 1}); webErrCode(f) != "bad_request" {
		t.Errorf("not an image: %v", f)
	}
	for _, a := range []webproto.ChatImageArgs{{I: i, N: 2}, {I: i + 5, N: 0}} {
		if f := r.call(webproto.OpChatImage, &a); webErrCode(f) != "not_found" {
			t.Errorf("%+v: %v", a, f)
		}
	}
}
