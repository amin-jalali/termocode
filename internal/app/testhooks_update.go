package app

import (
	"net/http"
	"sync"

	"github.com/amin-jalali/termocode/internal/update"
)

// UpdateSource stands for GitHub and the installed binary in tests
// (internal/testkit): the update code talks to HTTP at LatestURL, runs
// as release Version and replaces Exe (owned by no package manager).
type UpdateSource struct {
	HTTP      *http.Client
	LatestURL string
	Version   string
	Exe       string
}

var updateSourceMu sync.Mutex

// SetUpdateSource points the update code at s. It returns a restore
// func. For tests.
func SetUpdateSource(s UpdateSource) (restore func()) {
	updateSourceMu.Lock()
	defer updateSourceMu.Unlock()
	oldDeps, oldV := updateDepsFor, termocodeVersion
	updateDepsFor = func() updateDeps {
		d := oldDeps()
		d.client = update.Client{HTTP: s.HTTP, LatestURL: s.LatestURL, Version: s.Version}
		d.exe = func() (string, error) { return s.Exe, nil }
		d.owner = func(string) (string, bool) { return "", false }
		d.headlessOK = true
		return d
	}
	termocodeVersion = s.Version
	return func() {
		updateSourceMu.Lock()
		defer updateSourceMu.Unlock()
		updateDepsFor, termocodeVersion = oldDeps, oldV
	}
}

// ToastTexts returns the toasts on show as "title — detail" (just the
// title when there is no detail). For tests.
func (m Model) ToastTexts() []string {
	var out []string
	for _, t := range m.toast.Live() {
		s := t.Title
		if t.Detail != "" {
			s += " — " + t.Detail
		}
		out = append(out, s)
	}
	return out
}
