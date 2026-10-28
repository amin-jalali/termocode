package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestModelsCachePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	p, err := ModelsCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "termocode", "ai", "models.json"); p != want {
		t.Fatalf("path = %q, want %q", p, want)
	}
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", home)
	p, _ = ModelsCachePath()
	if want := filepath.Join(home, ".cache", "termocode", "ai", "models.json"); p != want {
		t.Fatalf("path = %q, want %q", p, want)
	}
}

func TestModelsCacheRoundTrip(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	c, err := LoadModelsCache()
	if err != nil || len(c) != 0 {
		t.Fatalf("missing file: %v %v", c, err)
	}
	now := time.Unix(1_800_000_000, 0)
	c.Put(KindAnthropic, []ModelInfo{{ID: "claude-opus-5-5", Title: "Claude Opus 5.5"}}, now)
	c.Put(KindOllama, []ModelInfo{{ID: "llama3.2:3b"}}, now)
	if err := SaveModelsCache(c); err != nil {
		t.Fatal(err)
	}
	got, err := LoadModelsCache()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[KindAnthropic].At != now.Unix() || !reflect.DeepEqual(got, c) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, c)
	}
	got.Drop(KindOllama)
	if _, ok := got[KindOllama]; ok {
		t.Fatal("Drop kept the entry")
	}
}

func TestModelsCacheCorruptIsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	p, _ := ModelsCachePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadModelsCache()
	if err != nil {
		t.Fatalf("corrupt file: err = %v", err)
	}
	if c == nil || len(c) != 0 {
		t.Fatalf("corrupt file: cache = %v", c)
	}
	// An empty map is still usable.
	c.Put(KindOpenAI, nil, time.Now())
}

func TestModelsCacheDueRefresh(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	c := ModelsCache{}
	if !c.DueRefresh(KindOpenAI, now) {
		t.Fatal("a missing entry is due")
	}
	c.Put(KindOpenAI, []ModelInfo{{ID: "gpt-5"}}, now)
	cases := []struct {
		age time.Duration
		due bool
	}{
		{0, false},
		{59 * time.Minute, false},
		{time.Hour, true},
		{24 * time.Hour, true},
		{-2 * time.Hour, true}, // written in the future: clock moved back
	}
	for _, tc := range cases {
		at := now.Add(tc.age)
		if got := c.DueRefresh(KindOpenAI, at); got != tc.due {
			t.Errorf("age %v: DueRefresh = %v, want %v", tc.age, got, tc.due)
		}
	}
}

func TestModelsCacheHoldsNoSecret(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	key := "sk-" + "ant-test-4444444444444444"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"claude-opus-5-5"}],"has_more":false}`)
	}))
	defer srv.Close()
	ms, err := ListModels(context.Background(), Config{Kind: KindAnthropic, BaseURL: srv.URL, APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := LoadModelsCache()
	c.Put(KindAnthropic, ms, time.Now())
	if err := SaveModelsCache(c); err != nil {
		t.Fatal(err)
	}
	p, _ := ModelsCachePath()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), key) || strings.Contains(string(data), srv.URL) {
		t.Fatalf("cache holds a secret or a URL: %s", data)
	}
	if !strings.Contains(string(data), "claude-opus-5-5") {
		t.Fatalf("cache misses the model: %s", data)
	}
}
