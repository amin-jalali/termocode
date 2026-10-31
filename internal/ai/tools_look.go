package ai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/pdftext"
)

// lookMaxImage is the largest image look_at sends (the APIs' limit).
const lookMaxImage = 5 << 20

// lookMaxPDF is the largest PDF look_at reads.
const lookMaxPDF = 32 << 20

// ImageMediaType is the media type of an image file by its extension
// ("" for other files).
func ImageMediaType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return ""
}

// LookAtTool is look_at (OMX-32): an image file goes to the model in one
// extra request with the question, and its answer comes back as text; a
// PDF's text is pulled out locally (no model request). see returns the
// provider and model to ask and whether that model takes images.
func LookAtTool(w Workspace, see func() (Provider, string, bool)) Tool {
	return Tool{
		Name: "look_at", Description: "Look at an image file (png, jpg, gif, webp) and answer a question about it, or read the text of a PDF file. " +
			"For an image this sends one extra request to the model. Use read_file for text files.",
		Params: map[string]any{
			"path":     strParam("image or PDF file, relative to the workspace"),
			"question": strParam("what you want to know (optional; default: describe it and copy its text)"),
		},
		Required: []string{"path"},
		Validate: w.argsInside("path"),
		Describe: func(a map[string]any) string {
			p := ArgString(a, "path")
			if ImageMediaType(p) != "" {
				return "Look at " + p + " (one extra model request)"
			}
			return "Read PDF " + p
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			abs, err := w.Resolve(ArgString(a, "path"))
			if err != nil {
				return "", err
			}
			rel := w.rel(abs)
			if SecretFile(abs) {
				return "", errSecretFile(rel)
			}
			mt := ImageMediaType(abs)
			isPDF := strings.EqualFold(filepath.Ext(abs), ".pdf")
			if mt == "" && !isPDF {
				return "", errors.New("look_at reads images (png, jpg, gif, webp) and PDF files; use read_file for " + rel)
			}
			fi, err := os.Stat(abs)
			if err != nil {
				return "", err
			}
			if isPDF {
				if fi.Size() > lookMaxPDF {
					return "", fmt.Errorf("%s is too big (%d MB; at most %d MB)", rel, fi.Size()>>20, lookMaxPDF>>20)
				}
				b, err := os.ReadFile(abs)
				if err != nil {
					return "", err
				}
				text, err := pdftext.Extract(b)
				if err != nil {
					return "", fmt.Errorf("%s: %w", rel, err)
				}
				if text == "" {
					return "no text found in " + rel + " (it may be scanned pages or images only)", nil
				}
				return "Text of " + rel + ":\n" + capOut(text, maxReadBytes), nil
			}
			p, model, vision := see()
			if p == nil || !vision {
				return "", errors.New("the current model can not see images; ask the user to describe " + rel + " or to pick a model with image input")
			}
			if fi.Size() > lookMaxImage {
				return "", fmt.Errorf("%s is too big for the model (%d MB; at most 5 MB)", rel, fi.Size()>>20)
			}
			b, err := os.ReadFile(abs)
			if err != nil {
				return "", err
			}
			q := strings.TrimSpace(ArgString(a, "question"))
			if q == "" {
				q = "Describe what this image shows. Copy any text in it word for word."
			}
			msgs := []Message{
				{Role: RoleSystem, Content: "You look at one image for another AI assistant and answer its question. Be exact and short. Copy text you see word for word. Say when you can not tell."},
				{Role: RoleUser, Content: "Image file: " + rel + "\nQuestion: " + q, Images: []Image{{MediaType: mt, Data: b}}},
			}
			var out strings.Builder
			for c := range p.Stream(ctx, Request{Messages: msgs, Model: model}) {
				out.WriteString(c.Delta)
				if c.Done && c.Err != nil {
					return "", c.Err
				}
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			ans := strings.TrimSpace(out.String())
			if ans == "" {
				return "", errors.New("the model gave no answer about " + rel)
			}
			return "About " + rel + " (answer of a separate look at the image):\n" + capOut(ans, maxOutBytes), nil
		},
	}
}
