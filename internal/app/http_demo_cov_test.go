package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The screenshot demo writes docs/users.http once (and keeps it out of
// git), opens it, and shows a canned answer: body, headers or a TLS
// failure; "file" only opens the file.
func TestCovHTTPDemoScenes(t *testing.T) {
	m, root, _ := covHTTP(t)
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.httpDemo("file")
	file := filepath.Join(root, "docs", "users.http")
	if got := mustRead(t, file); got != httpDemoFile {
		t.Fatalf("demo file:\n%s", got)
	}
	if got := mustRead(t, filepath.Join(root, ".git", "info", "exclude")); got != "/docs/users.http\n" {
		t.Errorf("exclude %q", got)
	}
	s := m.httpS()
	if s.shown != nil || !strings.HasPrefix(s.lens[file+"\x00getUser"].text, "200") {
		t.Errorf("file scene: shown %v lens %+v", s.shown, s.lens)
	}
	m.httpDemo("response")
	if s.shown == nil || s.view != httpViewBody || !strings.Contains(covSvcHTTPPage(t, m), "Ada Lovelace") {
		t.Errorf("response scene:\n%s", covSvcHTTPPage(t, m))
	}
	m.httpDemo("headers")
	if s.view != httpViewHeaders || !strings.Contains(covSvcHTTPPage(t, m), "x-request-id") {
		t.Errorf("headers scene:\n%s", covSvcHTTPPage(t, m))
	}
	m.httpDemo("tls")
	if !strings.Contains(covSvcHTTPPage(t, m), "certificate") {
		t.Errorf("tls scene:\n%s", covSvcHTTPPage(t, m))
	}
	// The file is written only once.
	if strings.Count(mustRead(t, filepath.Join(root, ".git", "info", "exclude")), "users.http") != 1 {
		t.Error("exclude written twice")
	}
	if dirExists(file) || !dirExists(root) {
		t.Error("dirExists")
	}
}
