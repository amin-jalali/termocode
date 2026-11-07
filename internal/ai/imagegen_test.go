package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var tinyPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func imageServer(t *testing.T, reply func(w http.ResponseWriter, body map[string]any)) (*httptest.Server, *map[string]any, *string) {
	t.Helper()
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" || r.Method != http.MethodPost {
			http.Error(w, "bad path "+r.URL.Path, http.StatusNotFound)
			return
		}
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		reply(w, got)
	}))
	t.Cleanup(srv.Close)
	return srv, &got, &auth
}

func TestGenerateImageWritesPNG(t *testing.T) {
	srv, got, auth := imageServer(t, func(w http.ResponseWriter, _ map[string]any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(tinyPNG)}}})
	})
	cfg := Config{Kind: KindOpenAI, APIKey: "sk-test", BaseURL: srv.URL + "/v1/"}
	img, err := GenerateImage(context.Background(), cfg, ImageRequest{Prompt: "a red fox", Size: "1024x1024"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(img, tinyPNG) {
		t.Fatalf("image %q", img)
	}
	if *auth != "Bearer sk-test" {
		t.Errorf("auth %q", *auth)
	}
	if (*got)["prompt"] != "a red fox" || (*got)["model"] != DefaultImageModel || (*got)["size"] != "1024x1024" || (*got)["n"] != float64(1) {
		t.Errorf("body %v", *got)
	}
}

func TestGenerateImageErrors(t *testing.T) {
	for name, reply := range map[string]func(w http.ResponseWriter, _ map[string]any){
		"http": func(w http.ResponseWriter, _ map[string]any) {
			http.Error(w, `{"error":{"message":"bad prompt"}}`, http.StatusBadRequest)
		},
		"url": func(w http.ResponseWriter, _ map[string]any) {
			_, _ = w.Write([]byte(`{"data":[{"url":"https://x/y.png"}]}`))
		},
		"empty": func(w http.ResponseWriter, _ map[string]any) { _, _ = w.Write([]byte(`{"data":[]}`)) },
		"notpng": func(w http.ResponseWriter, _ map[string]any) {
			_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("hello")) + `"}]}`))
		},
	} {
		srv, _, _ := imageServer(t, reply)
		_, err := GenerateImage(context.Background(), Config{Kind: KindOpenAI, APIKey: "k", BaseURL: srv.URL + "/v1"}, ImageRequest{Prompt: "p"})
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := GenerateImage(context.Background(), Config{Kind: KindOpenAI, APIKey: "k", BaseURL: "http://127.0.0.1:1"}, ImageRequest{Prompt: "  "}); err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Errorf("empty prompt: %v", err)
	}
	if _, err := GenerateImage(context.Background(), Config{Kind: KindOpenAI, APIKey: "k", BaseURL: "http://127.0.0.1:1"}, ImageRequest{Prompt: "p", Size: "7x7"}); err == nil || !strings.Contains(err.Error(), "size") {
		t.Errorf("bad size: %v", err)
	}
}

func TestImageGenSupported(t *testing.T) {
	for kind, want := range map[string]bool{KindOpenAI: true, KindCompatible: true, KindAnthropic: false, KindOllama: false, KindOpenRouter: false, KindNone: false} {
		if got := ImageGenSupported(kind); got != want {
			t.Errorf("%q: %v, want %v", kind, got, want)
		}
	}
}
