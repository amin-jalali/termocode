package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/crash"
)

// OpenTelemetry traces of tool calls (MCPX-38). Off unless the standard
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT (used as is) or
// OTEL_EXPORTER_OTLP_ENDPOINT (+ "/v1/traces") is set. Each tool call is
// one client span, sent as OTLP/HTTP JSON by a small exporter (no SDK);
// OTEL_EXPORTER_OTLP_HEADERS adds headers (an API key), never logged;
// OTEL_SERVICE_NAME names the service ("termocode"). The server gets the
// span as traceparent in the request's _meta (W3C trace context), so its
// own spans join the trace. A span names the server, the tool and the
// outcome; arguments and results never go into it.

const (
	otlpQueue = 256 // spans waiting; more are dropped
	otlpBatch = 64  // spans per POST
)

type span struct {
	traceID, spanID string
	name            string
	start, end      time.Time
	attrs           map[string]string
	errMsg          string // "" ok
}

type otlpExporter struct {
	url     string
	headers map[string]string
	service string
	queue   chan span
}

var otlp struct {
	sync.Mutex
	key string // the env the exporter was made from
	exp *otlpExporter
}

// tracer returns the exporter for the current environment (nil: off).
func tracer() *otlpExporter {
	u := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"))
	if u == "" {
		if base := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")); base != "" {
			u = strings.TrimRight(base, "/") + "/v1/traces"
		}
	}
	if u == "" || strings.EqualFold(os.Getenv("OTEL_SDK_DISABLED"), "true") {
		return nil
	}
	hdr := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS")
	if hdr == "" {
		hdr = os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")
	}
	svc := os.Getenv("OTEL_SERVICE_NAME")
	if svc == "" {
		svc = "termocode"
	}
	key := u + "\x00" + hdr + "\x00" + svc
	otlp.Lock()
	defer otlp.Unlock()
	if otlp.exp != nil && otlp.key == key {
		return otlp.exp
	}
	e := &otlpExporter{url: u, headers: parseOTLPHeaders(hdr), service: svc, queue: make(chan span, otlpQueue)}
	crash.Go("the trace exporter", e.run)
	otlp.key, otlp.exp = key, e
	return e
}

// parseOTLPHeaders reads "k=v,k2=v2" (values URL-encoded).
func parseOTLPHeaders(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(part, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			continue
		}
		if d, err := url.QueryUnescape(strings.TrimSpace(v)); err == nil {
			v = d
		}
		out[k] = v
	}
	return out
}

func randomID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type traceparentKey struct{}

// traceparentFrom is the W3C traceparent of ctx's span ("" none).
func traceparentFrom(ctx context.Context) string {
	s, _ := ctx.Value(traceparentKey{}).(string)
	return s
}

// traceCall runs one tool call in a span when tracing is on.
func traceCall(ctx context.Context, server, tool string, version func() string, run func(context.Context) (CallResult, error)) (CallResult, error) {
	e := tracer()
	if e == nil {
		return run(ctx)
	}
	sp := span{traceID: randomID(16), spanID: randomID(8), name: "tools/call " + tool, start: time.Now(),
		attrs: map[string]string{"mcp.method.name": "tools/call", "gen_ai.tool.name": tool,
			"gen_ai.operation.name": "execute_tool", "mcp.server.name": server}}
	res, err := run(context.WithValue(ctx, traceparentKey{}, "00-"+sp.traceID+"-"+sp.spanID+"-01"))
	sp.end = time.Now()
	if v := version(); v != "" {
		sp.attrs["mcp.protocol.version"] = v
	}
	switch {
	case err != nil:
		sp.errMsg = ai.RedactSecrets(ai.Redact(err.Error()))
		sp.attrs["error.type"] = "call_error"
	case res.IsError:
		sp.errMsg = "the tool reported an error"
		sp.attrs["error.type"] = "tool_error"
	}
	select {
	case e.queue <- sp:
	default: // the collector is slow or gone: drop the span
	}
	return res, err
}

func (e *otlpExporter) run() {
	hc := &http.Client{Timeout: 10 * time.Second}
	for sp := range e.queue {
		batch := []span{sp}
	more:
		for len(batch) < otlpBatch {
			select {
			case s := <-e.queue:
				batch = append(batch, s)
			default:
				break more
			}
		}
		body, err := json.Marshal(e.document(batch))
		if err != nil {
			continue
		}
		req, err := http.NewRequest(http.MethodPost, e.url, bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range e.headers {
			req.Header.Set(k, v)
		}
		if resp, err := hc.Do(req); err == nil {
			resp.Body.Close()
		}
	}
}

type otlpAttr struct {
	Key   string `json:"key"`
	Value struct {
		StringValue string `json:"stringValue"`
	} `json:"value"`
}

func attrs(m map[string]string) []otlpAttr {
	out := make([]otlpAttr, 0, len(m))
	for k, v := range m {
		a := otlpAttr{Key: k}
		a.Value.StringValue = v
		out = append(out, a)
	}
	return out
}

// document is the OTLP/HTTP JSON body of spans.
func (e *otlpExporter) document(spans []span) map[string]any {
	list := make([]map[string]any, 0, len(spans))
	for _, s := range spans {
		status := map[string]any{"code": 1} // ok
		if s.errMsg != "" {
			status = map[string]any{"code": 2, "message": s.errMsg}
		}
		list = append(list, map[string]any{"traceId": s.traceID, "spanId": s.spanID, "name": s.name, "kind": 3, // client
			"startTimeUnixNano": strconv.FormatInt(s.start.UnixNano(), 10), "endTimeUnixNano": strconv.FormatInt(s.end.UnixNano(), 10),
			"attributes": attrs(s.attrs), "status": status})
	}
	return map[string]any{"resourceSpans": []any{map[string]any{
		"resource":   map[string]any{"attributes": attrs(map[string]string{"service.name": e.service})},
		"scopeSpans": []any{map[string]any{"scope": map[string]any{"name": "termocode/mcp"}, "spans": list}},
	}}}
}

// serverVersion is the MCP version a running server speaks ("" none).
func (m *Manager) serverVersion(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st := m.servers[name]; st != nil && st.proc != nil && st.proc.Client != nil {
		return st.proc.Client.Version()
	}
	return ""
}
