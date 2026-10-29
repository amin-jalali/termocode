// Package termfont asks the host terminal to change its font size (the
// font_delta setting) and undoes it on exit.
//
// Few terminals let a program change the font size. Checked against each
// terminal's escape-code docs and source (2026-10):
//
//   - kitty: yes, through remote control (DCS "@kitty-cmd" set-font-size),
//     but only when kitty.conf allows it (allow_remote_control yes, or a
//     remote_control_password entry that allows set-font-size). With the
//     default (no) kitty refuses and says so.
//   - iTerm2, WezTerm, Ghostty, foot, Windows Terminal, GNOME Terminal and
//     the other VTE terminals, Alacritty, Konsole: no escape for it. (An
//     older version sent "OSC 1337 ; ChangeFontSize", which no terminal
//     knows: iTerm2's OSC 1337 parser has no such key.)
//
// So font_delta only works in kitty with remote control on. Elsewhere the
// UI shows a tip to use the terminal's own zoom.
package termfont

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Method is how the host terminal can change its font size.
type Method int

const (
	// None: the terminal has no way to do it.
	None Method = iota
	// Kitty: kitty remote control (set-font-size).
	Kitty
)

func (m Method) String() string {
	if m == Kitty {
		return "kitty"
	}
	return "none"
}

// Detect picks the method from the environment. kitty sets KITTY_WINDOW_ID
// (not forwarded over SSH) and TERM=xterm-kitty (usually forwarded). Inside
// tmux the escape does not reach the outer terminal.
func Detect(getenv func(string) string) Method {
	if getenv("TMUX") != "" {
		return None
	}
	if getenv("KITTY_WINDOW_ID") != "" || getenv("TERM") == "xterm-kitty" {
		return Kitty
	}
	return None
}

// kittyPrefix starts a kitty remote-control command (DCS body).
const kittyPrefix = "@kitty-cmd"

// maxStep is the largest font size change ParseKitty accepts.
const maxStep = 1000

type kittyCmd struct {
	Cmd        string       `json:"cmd"`
	Version    [3]int       `json:"version"`
	NoResponse bool         `json:"no_response"`
	Payload    kittyPayload `json:"payload"`
}

type kittyPayload struct {
	Size        int    `json:"size"`
	IncrementOp string `json:"increment_op,omitempty"`
}

// Request is the escape that changes the font by delta points. With reply
// the terminal answers (see KittyAnswer). "" when there is nothing to send.
func Request(m Method, delta int, reply bool) string {
	if m != Kitty || delta == 0 {
		return ""
	}
	c := kittyCmd{Cmd: "set-font-size", Version: [3]int{0, 14, 2}, NoResponse: !reply,
		Payload: kittyPayload{Size: delta, IncrementOp: "+"}}
	if delta < 0 {
		c.Payload.Size, c.Payload.IncrementOp = -delta, "-"
	}
	b, _ := json.Marshal(c)
	return "\x1bP" + kittyPrefix + string(b) + "\x1b\\"
}

// ParseKitty reads a DCS body ("@kitty-cmd{…}") and returns the font size
// change it asks for. ok is false for anything but a relative
// set-font-size.
func ParseKitty(body string) (delta int, ok bool) {
	js, found := strings.CutPrefix(body, kittyPrefix)
	if !found {
		return 0, false
	}
	var c struct {
		Cmd     string `json:"cmd"`
		Payload struct {
			Size        float64 `json:"size"`
			IncrementOp string  `json:"increment_op"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(js), &c) != nil || c.Cmd != "set-font-size" {
		return 0, false
	}
	// A step past maxStep is no real request; converting a huge float
	// to int would also flip its sign.
	if c.Payload.Size > maxStep || c.Payload.Size < -maxStep {
		return 0, false
	}
	n := int(c.Payload.Size)
	switch c.Payload.IncrementOp {
	case "+":
		return n, n != 0
	case "-":
		return -n, n != 0
	}
	return 0, false
}

// KittyAnswer looks for kitty's answer to a remote-control command in the
// bytes read from the terminal: answered once the whole answer is in, ok
// when kitty did it.
func KittyAnswer(b []byte) (answered, ok bool) {
	i := bytes.Index(b, []byte("\x1bP"+kittyPrefix))
	if i < 0 {
		return false, false
	}
	rest := b[i+2+len(kittyPrefix):]
	j := bytes.Index(rest, []byte("\x1b\\"))
	if j < 0 {
		return false, false
	}
	var r struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(rest[:j], &r) != nil {
		return true, false
	}
	return true, r.OK
}

// Querier writes q to the terminal and returns what it answers, reading
// until done says the answer is in or a timeout passes.
type Querier func(q string, done func([]byte) bool) []byte

// State is a font size change that the terminal made.
type State struct {
	method Method
	delta  int
}

// Change asks the terminal to change its font by delta points and checks
// that it did. Terminals with no way to do it are not asked at all.
func Change(getenv func(string) string, delta int, query Querier) State {
	m := Detect(getenv)
	if m == None || delta == 0 || query == nil {
		return State{}
	}
	reply := query(Request(m, delta, true), func(b []byte) bool {
		answered, _ := KittyAnswer(b)
		return answered
	})
	if _, ok := KittyAnswer(reply); !ok {
		return State{}
	}
	return State{method: m, delta: delta}
}

// Applied reports whether the terminal changed its font.
func (s State) Applied() bool { return s.delta != 0 }

// Restore is the escape that undoes the change ("" if none was made).
func (s State) Restore() string { return Request(s.method, -s.delta, false) }

// TipNeeded reports whether the user asked for a font change (want ≠ 0)
// that the terminal did not make.
func (s State) TipNeeded(want int) bool { return want != 0 && !s.Applied() }

// Tip is the toast shown when font_delta could not be applied.
const Tip = "Tip: use your terminal's zoom (Ctrl+-) to change the font size"

// String is for logs.
func (s State) String() string { return fmt.Sprintf("%v %+d", s.method, s.delta) }
