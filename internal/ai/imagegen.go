package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Image generation (CMP-97): one call to an OpenAI-compatible
// POST {base}/images/generations that returns base64 image data. The
// caller saves the PNG; this file only talks to the endpoint.

// DefaultImageModel is the model used when ai_image_model is unset.
const DefaultImageModel = "gpt-image-1"

// ImageSizes are the sizes the tool accepts ("" means the endpoint's
// default).
var ImageSizes = []string{"auto", "1024x1024", "1536x1024", "1024x1536"}

// maxImageResponse caps the JSON answer (base64 of a large PNG).
const maxImageResponse = 48 << 20

// imageTimeout bounds one generation (they take tens of seconds).
const imageTimeout = 3 * time.Minute

// ImageRequest is one generation.
type ImageRequest struct {
	Prompt string
	Model  string // "": DefaultImageModel
	Size   string // "": the endpoint's default
}

// ImageGenSupported reports whether provider kind has an images
// endpoint the tool can call.
func ImageGenSupported(kind string) bool {
	return kind == KindOpenAI || kind == KindCompatible
}

var pngMagic = []byte("\x89PNG\r\n\x1a\n")

// GenerateImage asks cfg's endpoint for one image and returns its PNG
// bytes.
func GenerateImage(ctx context.Context, cfg Config, r ImageRequest) ([]byte, error) {
	r.Prompt = strings.TrimSpace(r.Prompt)
	if r.Prompt == "" {
		return nil, errors.New("image prompt is empty")
	}
	if r.Size != "" && !slices.Contains(ImageSizes, r.Size) {
		return nil, fmt.Errorf("image size %q: use one of %s", r.Size, strings.Join(ImageSizes, ", "))
	}
	if r.Model == "" {
		r.Model = DefaultImageModel
	}
	base := cfg.BaseURL
	if base == "" {
		base = DefaultBaseURL(cfg.Kind)
	}
	if base == "" {
		return nil, errors.New("no images endpoint: set the provider's base URL")
	}
	body := map[string]any{"model": r.Model, "prompt": r.Prompt, "n": 1}
	if r.Size != "" {
		body["size"] = r.Size
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, imageTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, trimSlash(base)+"/images/generations", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: imageTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, requestError(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxImageResponse+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpError(resp.StatusCode, raw[:min(len(raw), 8192)])
	}
	if len(raw) > maxImageResponse {
		return nil, errors.New("image answer is too large")
	}
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("image answer: %w", err)
	}
	if len(out.Data) == 0 {
		return nil, errors.New("the endpoint returned no image")
	}
	if out.Data[0].B64 == "" {
		if out.Data[0].URL != "" {
			return nil, errors.New("the model returned a link, not image data; pick a model that returns image data (ai_image_model)")
		}
		return nil, errors.New("the endpoint returned no image data")
	}
	img, err := base64.StdEncoding.DecodeString(out.Data[0].B64)
	if err != nil {
		return nil, fmt.Errorf("image data: %w", err)
	}
	if !bytes.HasPrefix(img, pngMagic) {
		return nil, errors.New("the endpoint did not return a PNG image")
	}
	return img, nil
}
