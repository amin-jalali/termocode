package mcp

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Images from tool results (MCPX-11): a screenshot or a chart goes to a
// vision model as image input (ai.AttachImages); the text gets a short
// note in its place. Audio is never sent (no provider takes it in a
// tool result).

// MaxImageBytes caps one image sent to the model (the providers' limit).
const MaxImageBytes = 5 << 20

// MaxImages caps the images of one result.
const MaxImages = 4

// imageTypes are the image types the providers take.
var imageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// mediaNotes sends a result's images to the model when it can see them
// and returns each media item's note.
func mediaNotes(ctx context.Context) func(i int, c Content) string {
	sent := 0
	return func(_ int, c Content) string {
		if c.Type == "audio" {
			return "not shown: audio is not sent to the model"
		}
		mime := strings.ToLower(strings.TrimSpace(c.MimeType))
		if !imageTypes[mime] {
			return "not shown: the type is not supported"
		}
		if base64.StdEncoding.DecodedLen(len(c.Data)) > MaxImageBytes+3 {
			return "not shown: over the 5 MB limit"
		}
		data, err := base64.StdEncoding.DecodeString(c.Data)
		if err != nil || len(data) == 0 {
			return "not shown: bad data"
		}
		if len(data) > MaxImageBytes {
			return "not shown: over the 5 MB limit"
		}
		if sent >= MaxImages {
			return "not shown: more than 4 images"
		}
		if !ai.AttachImages(ctx, ai.Image{MediaType: mime, Data: data}) {
			return "not shown: this model does not take images"
		}
		sent++
		return "attached"
	}
}
