package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The gateway serves the embedded browser UI: the page and its script.
func TestGatewayServesTheBrowserUI(t *testing.T) {
	h := webAssets()
	if h == nil {
		t.Fatal("no browser UI built in")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "app.js") {
		t.Fatalf("GET / = %d %.200q", rec.Code, rec.Body.String())
	}
}
