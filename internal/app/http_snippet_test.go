package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// HTTP: Copy as Code… lists the languages; a pick copies that snippet.
func TestHTTPCopySnippetPicker(t *testing.T) {
	m, _, srv := covSvcHTTP(t)
	var clip []string
	defer SetClipboardSink(func(v string) { clip = append(clip, v) })()

	// Nothing shown and no .http buffer: a toast, no picker.
	m.dispatchHTTPPalette("http-copy-snippet")
	if m.pickerOpen {
		t.Fatal("picker opened without a request")
	}

	m.httpS().shown = &httpRun{rr: httpclient.Resolved{
		Request:  httpclient.Request{Method: "GET", URL: srv.URL + "/users"},
		Template: httpclient.Request{Method: "GET", URL: "{{host}}/users"}}}
	m.dispatchHTTPPalette("http-copy-snippet")
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPSnippet {
		t.Fatalf("no snippet picker: %q", covSvcToast(m))
	}
	if c, ok := m.handleHTTPPickerSelect("python"); !ok || c == nil {
		t.Fatalf("python pick: ok=%v", ok)
	}
	if len(clip) != 1 || !strings.Contains(clip[0], "import requests") || !strings.Contains(clip[0], srv.URL+"/users") {
		t.Fatalf("copied %q", clip)
	}
	// An unknown id copies nothing.
	m.handleHTTPPickerSelect("cobol")
	if len(clip) != 1 {
		t.Errorf("unknown lang copied %q", clip)
	}
}

// With a secret in the request, the snippet asks like Copy as curl, and
// the answer copies the chosen language.
func TestHTTPCopySnippetSecrets(t *testing.T) {
	m, _, _ := covSvcHTTP(t)
	var clip []string
	defer SetClipboardSink(func(v string) { clip = append(clip, v) })()
	tok := "sk-" + "test-snippet-42"
	m.httpS().shown = &httpRun{rr: httpclient.Resolved{
		Request:  httpclient.Request{Method: "GET", URL: "https://a.example/x", Headers: []httpclient.Header{{Name: "Authorization", Value: "Bearer " + tok}}},
		Template: httpclient.Request{Method: "GET", URL: "https://a.example/x", Headers: []httpclient.Header{{Name: "Authorization", Value: "Bearer {{token}}"}}},
		Secrets:  []string{tok}}}
	m.dispatchHTTPPalette("http-copy-snippet")
	m.handleHTTPPickerSelect("go")
	if !m.confirmOpen || m.confirmKind != confirmKindHTTPCopySecrets {
		t.Fatalf("no secrets card, clip %q", clip)
	}
	m.handleHTTPConfirm("cancel")
	m.handleHTTPConfirm("template")
	m.handleHTTPConfirm("secrets")
	if len(clip) != 2 || !strings.Contains(clip[0], "package main") || strings.Contains(clip[0], tok) ||
		!strings.Contains(clip[1], tok) || !strings.Contains(clip[1], "package main") {
		t.Errorf("clip = %q", clip)
	}
	// Copy as curl after a snippet still copies curl.
	m.httpCopyCurl(m.httpS().shown)
	m.handleHTTPConfirm("template")
	if len(clip) != 3 || !strings.HasPrefix(clip[2], "curl ") {
		t.Errorf("curl after snippet = %q", clip)
	}
}
