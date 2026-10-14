package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// EnvCapsFile is set in the UI's environment to the file where the server
// puts the caps of the last client that attached. The UI reads it on
// RepaintSignal.
const EnvCapsFile = "TERMOCODE_SESSION_CAPS"

// CapsEnvKeys are the environment variables that describe the client's
// terminal. The UI keeps the environment of the first client; on a later
// attach these are copied from the new client (a key the client does not
// set is unset in the UI).
var CapsEnvKeys = []string{
	"TERM", "COLORTERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION",
	"LANG", "LC_ALL", "LC_CTYPE", "LC_TERMINAL", "LC_TERMINAL_VERSION",
	"NO_COLOR", "TERMOCODE_COLORS", "TERMOCODE_ICON_MODE", "TERMOCODE_NERD_FONT",
	"KITTY_WINDOW_ID", "GHOSTTY_RESOURCES_DIR", "WEZTERM_EXECUTABLE", "WT_SESSION", "VTE_VERSION",
	// Whether the client runs over SSH (file transfer: a local copy, or
	// scp / kitty / iTerm2 to the client's machine).
	"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY",
}

// ClientCaps is what the attaching terminal can do. It travels as JSON
// after the size in the hello frame.
type ClientCaps struct {
	Env map[string]string `json:"env,omitempty"`
	// Graphics is the client's own graphics probe; nil when it did not
	// probe (images off, or a new session whose UI probes itself).
	Graphics *GraphicsCaps `json:"graphics,omitempty"`
	// Gen numbers the attach; the server sets it for the UI, which marks
	// its output for this terminal with AttachedSeq(Gen).
	Gen int `json:"gen,omitempty"`
}

// GraphicsCaps mirrors termimg.Caps with a stable protocol name, so a
// client and a server of different versions agree.
type GraphicsCaps struct {
	Protocol    string `json:"protocol"` // "kitty", "sixel", "blocks"
	CellW       int    `json:"cell_w,omitempty"`
	CellH       int    `json:"cell_h,omitempty"`
	InTmux      bool   `json:"in_tmux,omitempty"`
	Passthrough bool   `json:"passthrough,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// EnvCaps collects the terminal variables (CapsEnvKeys) that are set.
func EnvCaps(getenv func(string) string) ClientCaps {
	c := ClientCaps{Env: map[string]string{}}
	for _, k := range CapsEnvKeys {
		if v := getenv(k); v != "" {
			c.Env[k] = v
		}
	}
	return c
}

// encodeHello packs the hello payload: the size (the old format, all an
// old server reads), then the caps as JSON.
func encodeHello(cols, rows int, caps *ClientCaps) []byte {
	b := encodeSize(cols, rows)
	if caps == nil {
		return b
	}
	j, err := json.Marshal(caps)
	if err != nil || len(b)+len(j) > maxFrame {
		return b
	}
	return append(b, j...)
}

// decodeHello unpacks encodeHello. A hello from an old client (size only)
// or with caps we cannot read has nil caps.
func decodeHello(b []byte) (cols, rows int, caps *ClientCaps, err error) {
	cols, rows, err = decodeSize(b)
	if err != nil || len(b) == 4 {
		return cols, rows, nil, err
	}
	var c ClientCaps
	if json.Unmarshal(b[4:], &c) != nil {
		return cols, rows, nil, nil
	}
	return cols, rows, &c, nil
}

// writeCaps stores caps for the UI (mode 0600, replaced atomically); nil
// caps remove the file, so the UI never applies an older client's caps.
func writeCaps(path string, caps *ClientCaps) error {
	if caps == nil {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	b, err := json.Marshal(caps)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".caps-*")
	if err != nil {
		return err
	}
	_, werr := f.Write(b)
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(f.Name(), path)
	}
	if werr != nil {
		_ = os.Remove(f.Name())
	}
	return werr
}

// ReadCaps reads the caps the server stored; nil without an error when
// there are none.
func ReadCaps(path string) (*ClientCaps, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c ClientCaps
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
