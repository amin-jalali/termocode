//go:build browser

package web

import (
	"encoding/json"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Images (WEB-57): the page asks the hub for an image's bytes and shows
// them from a blob: URL it makes; never a URL from the hub.

// onePixelPNG is a real 1×1 PNG.
const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func imageRes(id uint64) *webproto.Res {
	return &webproto.Res{T: webproto.KindRes, ID: id, Data: raw(map[string]any{"mime": "image/png", "size": 70, "data": onePixelPNG})}
}

// An image file the editor cannot open as text shows as a picture.
func TestFileImagePreview(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openFiles(t, vp)
			r := goFile(t, p, c, "docs/shot.png")
			c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeLimit, Msg: "binary", Data: raw(map[string]any{"reason": "binary", "size": 70})})
			r = expectReq(t, c, "file.image")
			var a webproto.PathArgs
			if err := json.Unmarshal(r.Args, &a); err != nil || a.Path != "docs/shot.png" {
				t.Fatalf("file.image args %s", r.Args)
			}
			c.Send(t, imageRes(r.ID))
			p.WaitJS(filesView + `.querySelector(".img-preview img")?.complete && ` + filesView + `.querySelector(".img-preview img").naturalWidth === 1`)
			src := Eval[string](p, filesView+`.querySelector(".img-preview img").src`)
			if len(src) < 5 || src[:5] != "blob:" {
				t.Errorf("img src %q, want a blob: URL", src)
			}
			if alt := Eval[string](p, filesView+`.querySelector(".img-preview img").alt`); alt != "Image shot.png" {
				t.Errorf("alt %q", alt)
			}
			if probs := p.Problems(); len(probs) > 0 {
				t.Errorf("problems: %v", probs)
			}
		})
	}
}

// A picture too big for the page says so; a non-image binary file is not
// asked for.
func TestFileImageTooBigAndOtherBinary(t *testing.T) {
	p, c := openFiles(t, Phone)
	r := goFile(t, p, c, "big.jpg")
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeLimit, Msg: "binary", Data: raw(map[string]any{"reason": "binary", "size": 9e6})})
	r = expectReq(t, c, "file.image")
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeLimit, Msg: "too big"})
	p.WaitJS(filesView + `.querySelector(".img-err")?.textContent === "Too big to show here. Open it on the laptop."`)
	r = goFile(t, p, c, "tool.bin")
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeLimit, Msg: "binary", Data: raw(map[string]any{"reason": "binary", "size": 10})})
	p.WaitJS(filesView + `.innerText.includes("This is not a text file.")`)
	noReq(t, c, 300e6)
}

// An image attached to a chat message opens under it on a tap.
func TestChatImageOpensOnTap(t *testing.T) {
	m := userMsg(0, "look at this")
	m["images"] = []string{"shot.png"}
	p, c := openChat(t, Phone, chatWith(false, m))
	p.TapJS(chatView + `.querySelector(".msg-img-chip")`)
	r := expectReq(t, c, "chat.image")
	var a webproto.ChatImageArgs
	if err := json.Unmarshal(r.Args, &a); err != nil || a.I != 0 || a.N != 0 {
		t.Fatalf("chat.image args %s", r.Args)
	}
	c.Send(t, imageRes(r.ID))
	p.WaitJS(chatView + `.querySelector(".msg-img img")?.naturalWidth === 1`)
	// A second tap closes it and lets the blob go.
	p.TapJS(chatView + `.querySelector(".msg-img-chip")`)
	p.WaitJS(`!` + chatView + `.querySelector(".msg-img")`)
}
