package lspinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// askedFile lives in the tools root and remembers which filetypes the
// "no language server for X" toast has already been shown for, so it
// fires once per filetype — ever, not once per session.
const askedFile = "asked.json"

type askedState struct {
	Filetypes []string `json:"filetypes"`
}

func askedPath() (string, error) {
	root, err := ToolsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, askedFile), nil
}

// LoadAsked returns the set of filetypes already asked about. Missing or
// corrupt file → empty set.
func LoadAsked() map[string]bool {
	out := map[string]bool{}
	p, err := askedPath()
	if err != nil {
		return out
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return out
	}
	var s askedState
	if json.Unmarshal(b, &s) != nil {
		return out
	}
	for _, ft := range s.Filetypes {
		out[ft] = true
	}
	return out
}

// SaveAsked persists the asked set (best effort).
func SaveAsked(set map[string]bool) error {
	p, err := askedPath()
	if err != nil {
		return err
	}
	s := askedState{}
	for ft, ok := range set {
		if ok {
			s.Filetypes = append(s.Filetypes, ft)
		}
	}
	sort.Strings(s.Filetypes)
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// SuggestFor decides whether to suggest installing a language server when
// a buffer of filetype ft is opened. It returns the preferred tool and
// true when: ft is known, no server for it is available, and ft hasn't
// been asked about yet. available is injected for tests.
func SuggestFor(ft string, asked map[string]bool, available func(Tool) bool) (Tool, bool) {
	if ft == "" || asked[ft] {
		return Tool{}, false
	}
	cands := ForFiletype(CategoryLSP, ft)
	if len(cands) == 0 {
		return Tool{}, false
	}
	for _, t := range cands {
		if available(t) {
			return Tool{}, false
		}
	}
	return cands[0], true
}
