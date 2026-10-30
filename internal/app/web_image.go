package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Images for the page (WEB-57): a workspace image file (the file view) or
// an image the user attached to a message of the laptop's chat. The bytes
// go out as data the page shows from a blob: URL; the type comes from the
// first bytes, not the name, and only raster types the browser draws
// without running anything are sent (no SVG). Never a URL, never a
// terminal's output.

func init() {
	webOps[webproto.OpFileImage] = webOpFileImage
	webOps[webproto.OpChatImage] = webOpChatImage
}

// webImageMime is the type of an image by its first bytes, or "".
func webImageMime(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(b, []byte("\xff\xd8\xff")):
		return "image/jpeg"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return "image/gif"
	case len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "image/webp"
	}
	return ""
}

var (
	errWebNotImage = errors.New("not an image")
	errWebTooBig   = errors.New("image too big")
)

// webReadImage reads at most MaxImageBytes+1 bytes of a local file.
func webReadImage(abs string) ([]byte, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errWebNotImage
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, webproto.MaxImageBytes+1))
}

// webImageReply answers call with the image read (or why not).
func webImageReply(call *webCall, data []byte, err error) {
	switch {
	case errors.Is(err, os.ErrNotExist):
		call.fail(webproto.CodeNotFound, "That image does not exist any more.")
	case errors.Is(err, errWebNotImage):
		call.fail(webproto.CodeBadRequest, "This file is not an image the page can show.")
	case errors.Is(err, errWebTooBig) || len(data) > webproto.MaxImageBytes:
		call.fail(webproto.CodeLimit, "This image is too big to show here. Open it on the laptop.")
	case err != nil:
		call.fail(webproto.CodeNotFound, "That image cannot be read.")
	default:
		mime := webImageMime(data)
		if mime == "" {
			call.fail(webproto.CodeBadRequest, "This file is not an image the page can show.")
			return
		}
		call.reply(webproto.ImageRes{Mime: mime, Size: len(data), Data: base64.StdEncoding.EncodeToString(data)})
	}
}

func webOpFileImage(m *Model, call *webCall) tea.Cmd {
	abs, err := m.webResolve(call.args.(*webproto.PathArgs).Path)
	if err != nil {
		webFail(call, err)
		return nil
	}
	if ai.IsSecretFileName(abs) {
		webFail(call, webErrSecret())
		return nil
	}
	fsys, remote := m.remoteFS()
	call.later()
	return func() tea.Msg {
		var data []byte
		var err error
		if remote {
			var info os.FileInfo
			if info, err = fsys.Stat(context.Background(), abs); err == nil {
				switch {
				case !info.Mode().IsRegular():
					err = errWebNotImage
				case info.Size() > webproto.MaxImageBytes:
					err = errWebTooBig
				default:
					data, err = fsys.ReadFile(context.Background(), abs)
				}
			}
		} else {
			data, err = webReadImage(abs)
		}
		webImageReply(call, data, err)
		return nil
	}
}

// webOpChatImage reads image N of message I of the laptop's chat: a path
// the user attached on the laptop.
func webOpChatImage(m *Model, call *webCall) tea.Cmd {
	a := call.args.(*webproto.ChatImageArgs)
	s := m.aiS()
	if a.I >= len(s.conv.Messages) || a.N >= len(s.conv.Messages[a.I].Images) {
		call.fail(webproto.CodeNotFound, "That image is not in the chat.")
		return nil
	}
	abs := s.conv.Messages[a.I].Images[a.N]
	if ai.IsSecretFileName(abs) {
		webFail(call, webErrSecret())
		return nil
	}
	call.later()
	return func() tea.Msg {
		data, err := webReadImage(abs)
		webImageReply(call, data, err)
		return nil
	}
}
