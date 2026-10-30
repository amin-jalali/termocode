package webproto

// Images (WEB-57): an image file of the workspace or an image the user
// attached to a chat message, sent as bytes (base64 in Data) that the
// page shows from a blob: URL. Only PNG, JPEG, GIF and WebP, checked by
// their first bytes (never SVG: it can hold script), at most
// MaxImageBytes. Never a remote URL, never from a terminal.

// ChatImageArgs names image N of chat message I (the laptop's chat).
type ChatImageArgs struct {
	I int `json:"i"`
	N int `json:"n"`
}

// ImageRes is the result of file.image and chat.image.
type ImageRes struct {
	Mime string `json:"mime"`
	Size int    `json:"size"`
	Data string `json:"data"`
}

// MaxImageBytes caps an image the page can show (base64 makes it 4/3).
// One image answer stays well inside the hub's 4 MiB hard queue cap even
// with the other image op running.
const MaxImageBytes = 1 << 20

// ImageMimes are the only image types sent.
var ImageMimes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

func (a *ChatImageArgs) validate(int) error {
	if a.I < 0 || a.N < 0 || a.N > 64 {
		return invalid("bad message or image")
	}
	return nil
}

func init() {
	registerResult(OpFileImage, ImageRes{})
	registerResult(OpChatImage, ImageRes{})
}
