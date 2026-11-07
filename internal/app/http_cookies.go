package app

import (
	"fmt"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// HTTP cookies (API-11): one jar per environment, saved in
// ~/.config/termocode/http/cookies.json (0600). HTTP: Cookies… lists the
// active environment's cookies: Enter edits the value, Ctrl+D deletes,
// "Clear all cookies…" asks first.

const pickerKindHTTPCookies pickerKindEnum = 4511
const promptKindHTTPCookieValue promptKindEnum = 4511
const confirmKindHTTPClearCookies confirmKindEnum = 4511

func httpCookiesPath() string { return filepath.Join(httpConfigDir(), "cookies.json") }

// cookieStore is the saved cookie store, read on first use. A broken
// file starts empty (the next change writes a good one).
func (s *httpState) cookieStore() *httpclient.CookieStore {
	s.cookieMu.Lock()
	defer s.cookieMu.Unlock()
	if s.cookies == nil {
		s.cookies, _ = httpclient.LoadCookieStore(httpCookiesPath())
	}
	return s.cookies
}

// httpActiveEnv is the environment chosen for the current workspace.
func (m *Model) httpActiveEnv() string {
	dir := aiWorkspaceRoot()
	if f := m.httpCurrentFile(); f != "" {
		dir = filepath.Dir(f)
	}
	return httpSelectedEnv(httpRootFor(dir))
}

// httpCookieShort shows the start of a value only: the picker is for
// finding a cookie, the edit prompt shows the whole value.
func httpCookieShort(v string) string {
	r := []rune(v)
	if len(r) <= 6 {
		return v
	}
	return string(r[:4]) + "…"
}

func (m *Model) openHTTPCookies() tea.Cmd {
	s := m.httpS()
	env := m.httpActiveEnv()
	list := s.cookieStore().List(env)
	name := env
	if name == "" {
		name = "(none)"
	}
	if len(list) == 0 {
		return m.pushToastDetail(toast.Info, "No cookies for environment "+name, "Cookies that servers set are kept per environment.")
	}
	s.cookieEnv, s.cookieList = env, list
	now := time.Now()
	items := make([]picker.Item, 0, len(list)+1)
	for i, c := range list {
		hint := c.Domain + c.Path
		switch {
		case c.Expires.IsZero():
			hint += " · session"
		case c.Expires.Sub(now) < 48*time.Hour:
			hint += " · expires in " + c.Expires.Sub(now).Round(time.Minute).String()
		default:
			hint += " · expires " + c.Expires.Format("2006-01-02")
		}
		items = append(items, picker.Item{ID: fmt.Sprint(i), Title: c.Name + " = " + httpCookieShort(c.Value), Hint: hint})
	}
	items = append(items, picker.Item{ID: "__clear", Title: "Clear all cookies…"})
	return m.openHTTPPicker(pickerKindHTTPCookies, " HTTP Cookies · "+name+" ", items,
		[]string{"Enter edits the value · Ctrl+D deletes"})
}

// httpCookieAt is the listed cookie with picker id.
func (s *httpState) httpCookieAt(id string) (httpclient.Cookie, bool) {
	var i int
	if _, err := fmt.Sscan(id, &i); err != nil || i < 0 || i >= len(s.cookieList) {
		return httpclient.Cookie{}, false
	}
	return s.cookieList[i], true
}

// httpCookiesPickerSelect handles Enter in the cookies picker.
func (m *Model) httpCookiesPickerSelect(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindHTTPCookies {
		return nil, false
	}
	s := m.httpS()
	if id == "__clear" {
		name := s.cookieEnv
		if name == "" {
			name = "(none)"
		}
		m.confirm = confirm.New("Clear all cookies?", "Every cookie of environment "+name+" is deleted. Servers may ask you to sign in again.",
			[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "clear", Title: "Clear", Style: confirm.StyleDestructive}})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindHTTPClearCookies
		return nil, true
	}
	c, ok := s.httpCookieAt(id)
	if !ok {
		return nil, true
	}
	s.cookieEdit = c
	return m.openHTTPPrompt(promptKindHTTPCookieValue, "HTTP: Edit Cookie "+c.Name, "Value:", c.Value), true
}

// httpCookiesPickerKey handles Ctrl+D in the cookies picker.
func (m *Model) httpCookiesPickerKey(k tea.KeyMsg) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindHTTPCookies || k.String() != "ctrl+d" {
		return nil, false
	}
	it, ok := m.picker.Current()
	if !ok || it.ID == "__clear" {
		return nil, false
	}
	s := m.httpS()
	c, ok := s.httpCookieAt(it.ID)
	if !ok {
		return nil, true
	}
	s.cookieStore().Delete(s.cookieEnv, c.Domain, c.Path, c.Name)
	m.pickerOpen = false
	return m.openHTTPCookies(), true
}

// httpCookiesPrompt saves an edited value.
func (m *Model) httpCookiesPrompt(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindHTTPCookieValue {
		return nil, false
	}
	s := m.httpS()
	c := s.cookieEdit
	if c.Name == "" {
		return nil, true
	}
	c.Value = value
	if err := s.cookieStore().Set(s.cookieEnv, c); err != nil {
		return m.pushToastDetail(toast.Errr, "Cookie not saved", err.Error()), true
	}
	s.cookieEdit = httpclient.Cookie{}
	return m.pushToastDetail(toast.Info, "Cookie saved: "+c.Name, ""), true
}

// httpCookiesConfirm answers the "Clear all cookies?" card.
func (m *Model) httpCookiesConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindHTTPClearCookies {
		return nil, false
	}
	if id != "clear" {
		return nil, true
	}
	s := m.httpS()
	if err := s.cookieStore().Clear(s.cookieEnv); err != nil {
		return m.pushToastDetail(toast.Errr, "Clear failed", err.Error()), true
	}
	return m.pushToastDetail(toast.Info, "Cookies cleared", ""), true
}
