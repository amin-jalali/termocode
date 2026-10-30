package webproto

import (
	"math"
	"strings"
	"unicode/utf8"
)

// Limits of request args. The hub still checks paths with the workspace
// resolver and buffers against nvim; these rules refuse what is wrong in
// any workspace.
const (
	MaxPath        = 4096
	MaxPaths       = 1000
	MaxID          = 128
	MaxChoice      = 64
	MaxLabel       = 256
	MaxChatText    = 32 << 10
	MaxTermText    = 64 << 10
	MaxCommitMsg   = 16 << 10
	MaxSetText     = 1 << 20 // bytes of all lines of one set_text
	MaxSetLines    = 60000
	MaxSearchQuery = 1 << 10
	MaxSearchGlob  = 512
	MaxSearchHits  = 500
)

// validPath accepts a workspace-relative path: no control characters, not
// absolute, no ".." part. "" (the root) is allowed only when empty is true.
func validPath(p string, empty bool) bool {
	if p == "" {
		return empty
	}
	if len(p) > MaxPath || hasControl(p) || strings.HasPrefix(p, "/") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

func validBuf(b int) bool { return b > 0 && b <= math.MaxInt32 }

// validName is a non-empty id or label without control characters.
func validName(s string, max int) bool {
	return s != "" && len(s) <= max && !hasControl(s)
}

func (a *PathArgs) validate(int) error {
	if !validPath(a.Path, true) {
		return invalid("bad path")
	}
	return nil
}

func (a *PathsArgs) validate(int) error {
	if len(a.Paths) == 0 || len(a.Paths) > MaxPaths {
		return invalid("%d paths", len(a.Paths))
	}
	for _, p := range a.Paths {
		if !validPath(p, false) {
			return invalid("bad path")
		}
	}
	return nil
}

func (a *BufArgs) validate(int) error {
	if !validBuf(a.Buf) {
		return invalid("bad buffer")
	}
	return nil
}

func (a *TermHistoryArgs) validate(int) error {
	if !validBuf(a.Buf) || a.Before < 0 {
		return invalid("bad buffer or row")
	}
	return nil
}

func (a *SetTextArgs) validate(int) error {
	if !validBuf(a.Buf) || a.Tick < 0 {
		return invalid("bad buffer or tick")
	}
	if a.Start.Row < 0 || a.Start.Col < 0 || a.End.Row < 0 || a.End.Col < 0 {
		return invalid("negative position")
	}
	if a.End.Row < a.Start.Row || (a.End.Row == a.Start.Row && a.End.Col < a.Start.Col) {
		return invalid("end before start")
	}
	if len(a.Lines) > MaxSetLines {
		return invalid("%d lines", len(a.Lines))
	}
	n := 0
	for _, l := range a.Lines {
		if strings.ContainsAny(l, "\n\r") {
			return invalid("a line with a line break")
		}
		if !utf8.ValidString(l) {
			return invalid("a line that is not UTF-8")
		}
		n += len(l) + 1
	}
	if n > MaxSetText {
		return invalid("%d bytes of text", n)
	}
	return nil
}

func (a *TermSendArgs) validate(int) error {
	if !validBuf(a.Buf) {
		return invalid("bad buffer")
	}
	// Control characters are allowed: this is typing (Ctrl+C, Esc, Tab).
	if a.Text == "" || len(a.Text) > MaxTermText {
		return invalid("terminal text is %d bytes", len(a.Text))
	}
	return nil
}

func (a *ChatOpenArgs) validate(int) error {
	if !validName(a.ID, MaxID) {
		return invalid("bad chat id")
	}
	return nil
}

func (a *ChatGetMessageArgs) validate(int) error {
	if a.I < 0 {
		return invalid("bad message index")
	}
	return nil
}

func (a *ChatSendArgs) validate(int) error {
	if a.Text == "" || len(a.Text) > MaxChatText {
		return invalid("chat text is %d bytes", len(a.Text))
	}
	if a.ChatID != "" && !validName(a.ChatID, MaxID) {
		return invalid("bad chat id")
	}
	return nil
}

func (a *CardsAnswerArgs) validate(int) error {
	if !validName(a.ID, MaxID) || !validName(a.Choice, MaxChoice) {
		return invalid("bad card or choice")
	}
	return nil
}

func (a *GitDiffArgs) validate(int) error {
	if !validPath(a.Path, false) {
		return invalid("bad path")
	}
	return nil
}

func (a *GitShowArgs) validate(int) error {
	if len(a.Hash) < 4 || len(a.Hash) > 64 {
		return invalid("bad commit hash")
	}
	for _, c := range a.Hash {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return invalid("bad commit hash")
		}
	}
	return nil
}

func (a *GitCommitArgs) validate(int) error {
	if strings.TrimSpace(a.Message) == "" || len(a.Message) > MaxCommitMsg {
		return invalid("bad commit message")
	}
	return nil
}

func (a *SearchRunArgs) validate(int) error {
	if a.Query == "" || len(a.Query) > MaxSearchQuery {
		return invalid("query is %d bytes", len(a.Query))
	}
	if len(a.Include) > MaxSearchGlob || len(a.Exclude) > MaxSearchGlob ||
		hasControl(a.Include) || hasControl(a.Exclude) {
		return invalid("bad include or exclude")
	}
	if a.Max < 0 || a.Max > MaxSearchHits {
		return invalid("max is %d", a.Max)
	}
	return nil
}

func (a *SearchOpenArgs) validate(int) error {
	if !validPath(a.Path, false) || a.Line < 1 {
		return invalid("bad path or line")
	}
	return nil
}

func (a *LabelArgs) validate(int) error {
	if !validName(a.Label, MaxLabel) {
		return invalid("bad task label")
	}
	return nil
}

func (a *KeyArgs) validate(int) error {
	if !validName(a.Key, MaxID) {
		return invalid("bad run key")
	}
	return nil
}
