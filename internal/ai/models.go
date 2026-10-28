package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ModelInfo is one model a provider offers.
type ModelInfo struct {
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Created int64  `json:"created,omitempty"` // unix seconds; 0 when unknown
}

const (
	modelsTimeout = 10 * time.Second
	// MaxModels caps one provider's list (OpenRouter's is large).
	MaxModels     = 500
	modelsMaxBody = 16 << 20
)

// ListModels asks cfg's provider which models it serves: Anthropic's
// /v1/models, the OpenAI-style /models of every other kind, and Ollama's
// /api/tags with /models as the fallback. The ids come back sorted,
// without duplicates and capped at MaxModels. The key is registered with
// the redactor before the first request, and every error is one line
// with no key in it.
func ListModels(ctx context.Context, cfg Config) ([]ModelInfo, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL(cfg.Kind)
	}
	switch cfg.Kind {
	case KindAnthropic, KindClaudeOAuth, KindOpenAI, KindOpenRouter, KindOllama, KindLMStudio, KindCompatible:
	default:
		return nil, ErrNotConfigured
	}
	if cfg.BaseURL == "" {
		return nil, ErrNotConfigured
	}
	RegisterSecret(cfg.APIKey)
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: modelsTimeout}
	}
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	var ms []ModelInfo
	var err error
	switch cfg.Kind {
	case KindAnthropic, KindClaudeOAuth:
		ms, err = listAnthropicModels(ctx, cfg)
	case KindOllama:
		ms, err = listOllamaModels(ctx, cfg)
	default:
		ms, err = listOpenAIModels(ctx, cfg, trimSlash(cfg.BaseURL)+"/models")
	}
	if err != nil {
		return nil, oneLineError(err)
	}
	return tidyModels(ms), nil
}

// KeyRejected reports whether err is the provider refusing the key
// (HTTP 401 or 403).
func KeyRejected(err error) bool {
	var se statusError
	return errors.As(err, &se) && (se.code == http.StatusUnauthorized || se.code == http.StatusForbidden)
}

func listAnthropicModels(ctx context.Context, cfg Config) ([]ModelInfo, error) {
	var token string
	if cfg.Kind == KindClaudeOAuth {
		if cfg.Token == nil {
			return nil, ErrNotConfigured
		}
		t, err := cfg.Token(ctx)
		if err != nil || t == "" {
			if err == nil {
				err = errString("not signed in to Claude")
			}
			return nil, err
		}
		RegisterSecret(t)
		token = t
	}
	var out []ModelInfo
	after := ""
	// The first page plus one after_id page at most.
	for page := 0; page < 2; page++ {
		u := trimSlash(cfg.BaseURL) + "/v1/models?limit=1000"
		if after != "" {
			u += "&after_id=" + url.QueryEscape(after)
		}
		var body struct {
			Data []struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
				CreatedAt   string `json:"created_at"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		err := getModelsJSON(ctx, cfg, u, func(r *http.Request) {
			r.Header.Set("anthropic-version", anthropicVersion)
			if token != "" {
				r.Header.Set("Authorization", "Bearer "+token)
				r.Header.Set("anthropic-beta", anthropicOAuthBeta)
			} else {
				r.Header.Set("x-api-key", cfg.APIKey)
			}
		}, &body)
		if err != nil {
			return nil, err
		}
		for _, d := range body.Data {
			m := ModelInfo{ID: d.ID, Title: d.DisplayName}
			if t, err := time.Parse(time.RFC3339, d.CreatedAt); err == nil {
				m.Created = t.Unix()
			}
			out = append(out, m)
		}
		if !body.HasMore || body.LastID == "" {
			break
		}
		after = body.LastID
	}
	return out, nil
}

func listOpenAIModels(ctx context.Context, cfg Config, u string) ([]ModelInfo, error) {
	var body struct {
		Data []struct {
			ID      string `json:"id"`
			Name    string `json:"name"` // OpenRouter
			Created int64  `json:"created"`
		} `json:"data"`
	}
	err := getModelsJSON(ctx, cfg, u, func(r *http.Request) {
		if cfg.APIKey != "" {
			r.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		}
	}, &body)
	if err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(body.Data))
	for _, d := range body.Data {
		out = append(out, ModelInfo{ID: d.ID, Title: d.Name, Created: d.Created})
	}
	return out, nil
}

// listOllamaModels reads /api/tags on the host (the base URL without
// /v1); a server that does not answer it gets the OpenAI-style /models.
func listOllamaModels(ctx context.Context, cfg Config) ([]ModelInfo, error) {
	base := trimSlash(cfg.BaseURL)
	host := strings.TrimSuffix(base, "/v1")
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	err := getModelsJSON(ctx, cfg, host+"/api/tags", nil, &body)
	if err == nil {
		out := make([]ModelInfo, 0, len(body.Models))
		for _, m := range body.Models {
			out = append(out, ModelInfo{ID: m.Name})
		}
		return out, nil
	}
	if ms, ferr := listOpenAIModels(ctx, cfg, base+"/models"); ferr == nil {
		return ms, nil
	}
	return nil, err
}

// getModelsJSON GETs u and decodes a 2xx JSON answer into v.
func getModelsJSON(ctx context.Context, cfg Config, u string, auth func(*http.Request), v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return errString(Redact(err.Error()))
	}
	req.Header.Set("Accept", "application/json")
	if auth != nil {
		auth(req)
	}
	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return errString(Redact(err.Error()))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return httpError(resp.StatusCode, body)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, modelsMaxBody)).Decode(v); err != nil {
		return errString("the model list is not valid JSON")
	}
	return nil
}

// tidyModels sorts ms by id, drops empty and repeated ids (the first
// one with a title wins) and caps the list at MaxModels.
func tidyModels(ms []ModelInfo) []ModelInfo {
	byID := make(map[string]int, len(ms))
	out := make([]ModelInfo, 0, len(ms))
	for _, m := range ms {
		m.ID = strings.TrimSpace(m.ID)
		if m.ID == "" {
			continue
		}
		if i, ok := byID[m.ID]; ok {
			if out[i].Title == "" {
				out[i].Title = m.Title
			}
			if out[i].Created == 0 {
				out[i].Created = m.Created
			}
			continue
		}
		byID[m.ID] = len(out)
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > MaxModels {
		out = out[:MaxModels]
	}
	return out
}

// oneLineError folds err's text onto one line, keeping a statusError's
// code for KeyRejected.
func oneLineError(err error) error {
	msg := Redact(strings.Join(strings.Fields(err.Error()), " "))
	var se statusError
	if errors.As(err, &se) {
		return statusError{code: se.code, msg: msg}
	}
	return errString(msg)
}
