package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// httpCookiesSetup is the covSvcHTTP workspace with "dev" as the active
// environment and two dev cookies plus one prod cookie.
func httpCookiesSetup(t *testing.T) (*Model, string) {
	t.Helper()
	m, root, _ := covSvcHTTP(t)
	if err := httpSetSelectedEnv(httpRootFor(root), "dev"); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse("https://api.example.com/")
	store := m.httpS().cookieStore()
	store.Jar("dev").SetCookies(u, []*http.Cookie{{Name: "sid", Value: "abcdef123456789"}, {Name: "theme", Value: "dark"}})
	store.Jar("prod").SetCookies(u, []*http.Cookie{{Name: "prodonly", Value: "p"}})
	return m, root
}

func TestHTTPCookiesPickerListsTheActiveEnvironment(t *testing.T) {
	m, _ := httpCookiesSetup(t)
	m.dispatchHTTPPalette("http-cookies")
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPCookies {
		t.Fatalf("no cookies picker: %q", covSvcToast(m))
	}
	got := covSvcPickerText(m)
	for _, want := range []string{"dev", "sid", "theme", "api.example.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("picker misses %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "prodonly") {
		t.Errorf("picker shows a prod cookie:\n%s", got)
	}
	if strings.Contains(got, "abcdef123456789") {
		t.Errorf("picker shows the full cookie value:\n%s", got)
	}
}

func TestHTTPCookiesEditValue(t *testing.T) {
	m, _ := httpCookiesSetup(t)
	m.dispatchHTTPPalette("http-cookies")
	it, ok := m.picker.Current()
	if !ok {
		t.Fatal("no current item")
	}
	m.pickerOpen = false
	m.handleHTTPPickerSelect(it.ID)
	if !m.promptOpen || m.promptKind != promptKindHTTPCookieValue {
		t.Fatalf("no edit prompt (kind %v)", m.promptKind)
	}
	m.promptOpen = false
	m.handleHTTPPromptSubmit("newvalue")
	list := m.httpS().cookieStore().List("dev")
	if len(list) != 2 || list[0].Name != "sid" || list[0].Value != "newvalue" {
		t.Fatalf("after edit = %+v", list)
	}
	if !strings.Contains(covSvcToast(m), "Cookie saved") {
		t.Errorf("toast = %q", covSvcToast(m))
	}
}

func TestHTTPCookiesDeleteAndClear(t *testing.T) {
	m, _ := httpCookiesSetup(t)
	m.dispatchHTTPPalette("http-cookies")
	if _, ok := m.httpPickerKey(tea.KeyMsg{Type: tea.KeyCtrlD}); !ok {
		t.Fatal("ctrl+d not handled")
	}
	if list := m.httpS().cookieStore().List("dev"); len(list) != 1 || list[0].Name != "theme" {
		t.Fatalf("after ctrl+d = %+v", list)
	}
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPCookies {
		t.Fatal("picker not shown again after delete")
	}
	m.pickerOpen = false
	m.handleHTTPPickerSelect("__clear")
	if !m.confirmOpen || m.confirmKind != confirmKindHTTPClearCookies {
		t.Fatal("no clear card")
	}
	if !strings.Contains(cardText(m), "Cancel") {
		t.Fatalf("card = %s", cardText(m))
	}
	m.handleHTTPConfirm("cancel")
	if len(m.httpS().cookieStore().List("dev")) != 1 {
		t.Fatal("cancel cleared the cookies")
	}
	m.handleHTTPPickerSelect("__clear")
	m.handleHTTPConfirm("clear")
	if len(m.httpS().cookieStore().List("dev")) != 0 {
		t.Fatal("clear kept cookies")
	}
	if len(m.httpS().cookieStore().List("prod")) != 1 {
		t.Fatal("clear of dev removed prod cookies")
	}
	m.dispatchHTTPPalette("http-cookies")
	if m.pickerOpen && m.pickerKind == pickerKindHTTPCookies {
		if got := covSvcPickerText(m); strings.Contains(got, "sid") || strings.Contains(got, "theme") {
			t.Errorf("empty picker still lists cookies:\n%s", got)
		}
	} else if !strings.Contains(covSvcToast(m), "No cookies") {
		t.Errorf("empty toast = %q", covSvcToast(m))
	}
}

func TestHTTPCookiesClearCardIsSafeFirst(t *testing.T) {
	m, _ := httpCookiesSetup(t)
	m.dispatchHTTPPalette("http-cookies")
	m.pickerOpen = false
	m.handleHTTPPickerSelect("__clear")
	if !m.confirmOpen {
		t.Fatal("no card")
	}
	if b := m.confirm.Buttons(); len(b) < 2 || b[0].ID != "cancel" {
		t.Fatalf("buttons = %+v, want Cancel first", b)
	}
}

func TestHTTPCookiesPersistInPrivateFile(t *testing.T) {
	m, _ := httpCookiesSetup(t)
	path := filepath.Join(httpConfigDir(), "cookies.json")
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("cookies not saved: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", st.Mode().Perm())
	}
	m.http = nil // a new session reads the file again
	if got := m.httpS().cookieStore().List("dev"); len(got) != 2 {
		t.Fatalf("reloaded dev = %+v", got)
	}
}

func TestHTTPClientSavesCookiesUnderTheRequestEnvironment(t *testing.T) {
	m, _ := httpCookiesSetup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "fresh", Value: "1", Path: "/"})
	}))
	defer srv.Close()
	c := m.httpS().httpClient(loadHTTPSettings())
	r := httpclient.Request{Method: "GET", URL: srv.URL + "/login"}
	if resp := c.Send(context.Background(), httpclient.Resolved{Request: r, Template: r, Env: "staging"}, httpclient.Hooks{}); resp.Err != "" {
		t.Fatal(resp.Err)
	}
	list := m.httpS().cookieStore().List("staging")
	if len(list) != 1 || list[0].Name != "fresh" {
		t.Fatalf("staging cookies = %+v", list)
	}
}
