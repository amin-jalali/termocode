package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
)

type otlpSink struct {
	mu     sync.Mutex
	bodies []string
	auth   []string
	paths  []string
}

func newOTLPSink(t *testing.T) (*otlpSink, *httptest.Server) {
	s := &otlpSink{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, string(b))
		s.auth = append(s.auth, r.Header.Get("Api-Key"))
		s.paths = append(s.paths, r.URL.Path+" "+r.Header.Get("Content-Type"))
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return s, srv
}

func (s *otlpSink) all() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.bodies, "\n")
}

// Spec (MCPX-38, OTel): with OTEL_EXPORTER_OTLP_ENDPOINT set, each tool
// call is one client span sent to <endpoint>/v1/traces as OTLP/HTTP
// JSON (with OTEL_EXPORTER_OTLP_HEADERS); the server gets the span's
// traceparent in _meta. Arguments and results never go into the span.
func TestOTelSpanPerToolCall(t *testing.T) {
	sink, srv := newOTLPSink(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "api-key=k%3D1")
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	m := e.manager(t, 0, nil)
	defer m.Close()
	if _, err := m.Call(context.Background(), nil, "fake", "echo", map[string]any{"text": "hidden-arg"}); err != nil {
		t.Fatal(err)
	}
	_, _ = m.Call(context.Background(), nil, "fake", "fails", nil)
	waitUntil(t, "two exported spans", func() bool {
		return strings.Count(sink.all(), `"name":"tools/call `) >= 2
	})
	got := sink.all()
	for _, want := range []string{`"name":"tools/call echo"`, `"name":"tools/call fails"`, `"stringValue":"termocode"`,
		`"key":"mcp.method.name"`, `"key":"gen_ai.tool.name"`, `"stringValue":"fake"`, `"kind":3`, `"code":2`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	if strings.Contains(got, "hidden-arg") || strings.Contains(got, "echo: ") {
		t.Fatalf("arguments or results in the span: %s", got)
	}
	sink.mu.Lock()
	auth, path := sink.auth[0], sink.paths[0]
	sink.mu.Unlock()
	if auth != "k=1" || path != "/v1/traces application/json" {
		t.Fatalf("header %q path %q", auth, path)
	}
	var doc struct {
		ResourceSpans []struct {
			ScopeSpans []struct {
				Spans []struct {
					Name    string `json:"name"`
					TraceID string `json:"traceId"`
					SpanID  string `json:"spanId"`
				} `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	first := strings.SplitN(got, "\n", 2)[0]
	if err := json.Unmarshal([]byte(first), &doc); err != nil || len(doc.ResourceSpans) == 0 {
		t.Fatalf("bad OTLP JSON %v: %s", err, first)
	}
	sp := doc.ResourceSpans[0].ScopeSpans[0].Spans[0]
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(sp.TraceID) || !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(sp.SpanID) {
		t.Fatalf("ids %+v", sp)
	}
	b, _ := os.ReadFile(e.log)
	if !strings.Contains(string(b), "traceparent 00-"+sp.TraceID+"-"+sp.SpanID+"-01") {
		t.Fatalf("the server did not get the traceparent of %+v:\n%s", sp, b)
	}
}

// Spec (MCPX-38, OTel): without the variable nothing is traced or sent.
func TestOTelOffWithoutEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	m := e.manager(t, 0, nil)
	defer m.Close()
	if _, err := m.Call(context.Background(), nil, "fake", "echo", map[string]any{"text": "x"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(e.log); strings.Contains(string(b), "traceparent") {
		t.Fatalf("a traceparent without tracing:\n%s", b)
	}
}
