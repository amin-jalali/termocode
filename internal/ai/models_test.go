package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// modelIDs returns the ids of ms in order.
func modelIDs(ms []ModelInfo) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}

func TestListModelsAnthropic(t *testing.T) {
	key := "sk-" + "ant-test-0000000000000000"
	var pages int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("x-api-key") != key || r.Header.Get("anthropic-version") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		pages++
		if r.URL.Query().Get("after_id") == "" {
			fmt.Fprint(w, `{"data":[{"id":"claude-sonnet-5-5","display_name":"Claude Sonnet 5.5","created_at":"2026-01-02T03:04:05Z"},
				{"id":"claude-opus-5-5","display_name":"Claude Opus 5.5"}],"has_more":true,"last_id":"claude-opus-5-5"}`)
			return
		}
		if r.URL.Query().Get("after_id") != "claude-opus-5-5" {
			t.Errorf("after_id = %q", r.URL.Query().Get("after_id"))
		}
		fmt.Fprint(w, `{"data":[{"id":"claude-haiku-4-5"},{"id":"claude-opus-5-5"}],"has_more":true,"last_id":"claude-haiku-4-5"}`)
	}))
	defer srv.Close()
	ms, err := ListModels(context.Background(), Config{Kind: KindAnthropic, BaseURL: srv.URL, APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := modelIDs(ms), []string{"claude-haiku-4-5", "claude-opus-5-5", "claude-sonnet-5-5"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	if pages != 2 {
		t.Fatalf("pages = %d, want 2 (one after_id page at most)", pages)
	}
	if ms[2].Title != "Claude Sonnet 5.5" || ms[2].Created == 0 {
		t.Fatalf("sonnet = %+v", ms[2])
	}
}

func TestListModelsWrongKeyIsOneLineWithoutKey(t *testing.T) {
	key := "sk-" + "ant-test-1111111111111111"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		// A server that echoes the key back, over several lines.
		fmt.Fprintf(w, "{\"error\":{\"message\":\"invalid x-api-key\\n%s\\nplease check\"}}", r.Header.Get("x-api-key"))
	}))
	defer srv.Close()
	_, err := ListModels(context.Background(), Config{Kind: KindAnthropic, BaseURL: srv.URL, APIKey: key})
	if err == nil {
		t.Fatal("want an error")
	}
	msg := err.Error()
	if strings.Contains(msg, key) || strings.Contains(msg, "1111111111") {
		t.Fatalf("error holds the key: %q", msg)
	}
	if strings.Contains(msg, "\n") {
		t.Fatalf("error is not one line: %q", msg)
	}
	if !KeyRejected(err) {
		t.Fatalf("KeyRejected(%q) = false", msg)
	}
}

func TestListModelsRedactsEchoedKeyOpenAI(t *testing.T) {
	// No well-known prefix: only the registered secret hides it.
	key := "plain" + "secretvalue-abcdef"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(w, `{"error":"bad key %s"}`, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	}))
	defer srv.Close()
	_, err := ListModels(context.Background(), Config{Kind: KindCompatible, BaseURL: srv.URL, APIKey: key})
	if err == nil || strings.Contains(err.Error(), key) {
		t.Fatalf("err = %v", err)
	}
	if !KeyRejected(err) {
		t.Fatalf("403 should be a rejected key: %v", err)
	}
}

func TestListModelsOpenAIStyle(t *testing.T) {
	for _, kind := range []string{KindOpenAI, KindOpenRouter, KindLMStudio, KindCompatible} {
		t.Run(kind, func(t *testing.T) {
			key := ""
			if kind == KindOpenAI || kind == KindOpenRouter {
				key = "sk-" + "test-2222222222222222"
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/models" {
					http.NotFound(w, r)
					return
				}
				auth := r.Header.Get("Authorization")
				if key != "" && auth != "Bearer "+key {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if key == "" && auth != "" {
					t.Errorf("Authorization sent without a key: %q", auth)
				}
				fmt.Fprint(w, `{"data":[{"id":"zeta","created":1700000000},{"id":"alpha","name":"Alpha One"},{"id":"zeta"},{"id":""}]}`)
			}))
			defer srv.Close()
			ms, err := ListModels(context.Background(), Config{Kind: kind, BaseURL: srv.URL + "/v1/", APIKey: key})
			if err != nil {
				t.Fatal(err)
			}
			if got, want := modelIDs(ms), []string{"alpha", "zeta"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("ids = %v, want %v", got, want)
			}
			if ms[0].Title != "Alpha One" || ms[1].Created != 1700000000 {
				t.Fatalf("models = %+v", ms)
			}
		})
	}
}

func TestListModelsOllamaTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("path = %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"models":[{"name":"qwen2.5-coder:7b"},{"name":"llama3.2:3b"}]}`)
	}))
	defer srv.Close()
	ms, err := ListModels(context.Background(), Config{Kind: KindOllama, BaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := modelIDs(ms), []string{"llama3.2:3b", "qwen2.5-coder:7b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
}

func TestListModelsOllamaFallsBackToModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"phi4"}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	ms, err := ListModels(context.Background(), Config{Kind: KindOllama, BaseURL: srv.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := modelIDs(ms); !reflect.DeepEqual(got, []string{"phi4"}) {
		t.Fatalf("ids = %v", got)
	}
}

func TestListModelsCapsAt500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString(`{"data":[`)
		for i := 0; i < 700; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"id":"m%04d"}`, i)
		}
		b.WriteString(`]}`)
		fmt.Fprint(w, b.String())
	}))
	defer srv.Close()
	ms, err := ListModels(context.Background(), Config{Kind: KindCompatible, BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 500 || ms[0].ID != "m0000" || ms[499].ID != "m0499" {
		t.Fatalf("len = %d first %q", len(ms), ms[0].ID)
	}
}

func TestListModelsNotConfigured(t *testing.T) {
	for _, cfg := range []Config{{Kind: KindNone}, {Kind: KindCompatible}, {Kind: "nope", BaseURL: "http://x"}} {
		if _, err := ListModels(context.Background(), cfg); err == nil {
			t.Errorf("%+v: want an error", cfg)
		}
	}
}

func TestListModelsNetworkErrorRedacted(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // connection refused from now on
	key := "sk-" + "test-3333333333333333"
	_, err := ListModels(context.Background(), Config{Kind: KindOpenAI, BaseURL: url, APIKey: key})
	if err == nil || strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "\n") {
		t.Fatalf("err = %v", err)
	}
	if KeyRejected(err) {
		t.Fatal("a network error is not a rejected key")
	}
}
