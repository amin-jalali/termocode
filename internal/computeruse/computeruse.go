// Package computeruse lets the AI look at and drive the desktop (CMP-90):
// a screenshot, a mouse click, typed text, a key combination. It records
// nothing on its own and links no native library: each action starts one
// outside program with plain arguments (no shell). The app offers the
// tools only when ai_computer_use is on, and every call asks the user
// first (docs/adr/0023-computer-use.md).
package computeruse

import (
	"context"
	"errors"
	"fmt"
	"github.com/amin-jalali/termocode/internal/lazyre"
	"strconv"
	"strings"
	"unicode"
)

// Limits of one action.
const (
	MaxCoord = 20000 // the largest x or y in pixels
	MaxText  = 2000  // runes typed in one call
)

// Driver does the actions on one kind of desktop.
type Driver interface {
	Name() string
	Screenshot(ctx context.Context, path string) error // writes a PNG
	Click(ctx context.Context, x, y int, button string) error
	Type(ctx context.Context, text string) error
	Key(ctx context.Context, keys string) error
}

// Env is what Detect looks at (seams for tests).
type Env struct {
	OS       string // runtime.GOOS
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Run      func(ctx context.Context, name string, args ...string) error
}

// Buttons are the mouse buttons a click may use.
var Buttons = []string{"left", "middle", "right"}

var keysRe = lazyre.New(`^[A-Za-z0-9_]+(\+[A-Za-z0-9_]+)*$`)

// CheckPoint rejects a point off any screen.
func CheckPoint(x, y int) error {
	if x < 0 || y < 0 || x > MaxCoord || y > MaxCoord {
		return fmt.Errorf("the point %d,%d is off the screen (0…%d)", x, y, MaxCoord)
	}
	return nil
}

// CheckButton rejects an unknown mouse button.
func CheckButton(b string) error {
	for _, k := range Buttons {
		if b == k {
			return nil
		}
	}
	return fmt.Errorf("button must be one of %s", strings.Join(Buttons, ", "))
}

// CheckKeys accepts a combination like "ctrl+shift+t" or "Return".
func CheckKeys(k string) error {
	if !keysRe.MatchString(k) {
		return fmt.Errorf("keys %q: names joined by + (like ctrl+s or Return)", k)
	}
	return nil
}

// CheckText rejects empty or long text and control characters other
// than newline and tab.
func CheckText(s string) error {
	if s == "" {
		return errors.New("no text to type")
	}
	if n := len([]rune(s)); n > MaxText {
		return fmt.Errorf("the text has %d characters; at most %d in one call", n, MaxText)
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return fmt.Errorf("the text has a control character (%U)", r)
		}
	}
	return nil
}

// Detect picks the driver for this desktop, or says what is missing.
func Detect(e Env) (Driver, error) {
	has := func(n string) bool { _, err := e.LookPath(n); return err == nil }
	switch e.OS {
	case "linux", "freebsd", "openbsd", "netbsd":
		if e.Getenv("WAYLAND_DISPLAY") != "" {
			if !has("grim") {
				return nil, errors.New("computer use on Wayland needs grim for screenshots (and ydotool to click and type)")
			}
			d := &cliDriver{name: "Wayland (grim, ydotool)", run: e.Run, shot: []string{"grim"}}
			if has("ydotool") {
				d.input = "ydotool"
			}
			d.missing = "ydotool (and its ydotoold service)"
			return d, nil
		}
		if e.Getenv("DISPLAY") == "" {
			return nil, errors.New("no desktop here (no DISPLAY or WAYLAND_DISPLAY): computer use needs a graphical session")
		}
		d := &cliDriver{name: "X11 (xdotool)", run: e.Run, missing: "xdotool"}
		switch {
		case has("scrot"):
			d.shot = []string{"scrot", "-o"}
		case has("import"):
			d.shot = []string{"import", "-window", "root"}
		}
		if has("xdotool") {
			d.input = "xdotool"
		}
		if d.shot == nil && d.input == "" {
			return nil, errors.New("computer use on X11 needs xdotool (click, type, keys) and scrot or ImageMagick's import (screenshots)")
		}
		return d, nil
	case "darwin":
		d := &cliDriver{name: "macOS (screencapture, cliclick)", run: e.Run, shot: []string{"screencapture", "-x"}, missing: "cliclick"}
		if has("cliclick") {
			d.input = "cliclick"
		}
		return d, nil
	}
	return nil, fmt.Errorf("computer use is not supported on %s yet", e.OS)
}

// cliDriver runs outside programs: shot is the screenshot argv (the path
// is added), input the program that clicks and types.
type cliDriver struct {
	name    string
	run     func(ctx context.Context, name string, args ...string) error
	shot    []string
	input   string // "xdotool", "ydotool", "cliclick" or "" (none)
	missing string // what to install for input
}

func (d *cliDriver) Name() string { return d.name }

func (d *cliDriver) exec(ctx context.Context, argv ...string) error {
	if err := d.run(ctx, argv[0], argv[1:]...); err != nil {
		return fmt.Errorf("%s: %w", argv[0], err)
	}
	return nil
}

func (d *cliDriver) Screenshot(ctx context.Context, path string) error {
	if d.shot == nil {
		return errors.New("no screenshot program: install scrot or ImageMagick")
	}
	return d.exec(ctx, append(append([]string{}, d.shot...), path)...)
}

func (d *cliDriver) needInput() error {
	if d.input == "" {
		return fmt.Errorf("clicking and typing need %s", d.missing)
	}
	return nil
}

func (d *cliDriver) Click(ctx context.Context, x, y int, button string) error {
	if err := d.needInput(); err != nil {
		return err
	}
	if err := CheckPoint(x, y); err != nil {
		return err
	}
	if err := CheckButton(button); err != nil {
		return err
	}
	xs, ys := strconv.Itoa(x), strconv.Itoa(y)
	switch d.input {
	case "xdotool":
		n := map[string]string{"left": "1", "middle": "2", "right": "3"}[button]
		return d.exec(ctx, "xdotool", "mousemove", xs, ys, "click", n)
	case "ydotool":
		if err := d.exec(ctx, "ydotool", "mousemove", "--absolute", "-x", xs, "-y", ys); err != nil {
			return err
		}
		n := map[string]string{"left": "0xC0", "right": "0xC1", "middle": "0xC2"}[button]
		return d.exec(ctx, "ydotool", "click", n)
	}
	op := map[string]string{"left": "c:", "right": "rc:", "middle": "c:"}[button]
	return d.exec(ctx, "cliclick", op+xs+","+ys)
}

func (d *cliDriver) Type(ctx context.Context, text string) error {
	if err := d.needInput(); err != nil {
		return err
	}
	if err := CheckText(text); err != nil {
		return err
	}
	switch d.input {
	case "xdotool":
		return d.exec(ctx, "xdotool", "type", "--delay", "12", "--", text)
	case "ydotool":
		return d.exec(ctx, "ydotool", "type", "--", text)
	}
	return d.exec(ctx, "cliclick", "t:"+text)
}

func (d *cliDriver) Key(ctx context.Context, keys string) error {
	if err := d.needInput(); err != nil {
		return err
	}
	if err := CheckKeys(keys); err != nil {
		return err
	}
	switch d.input {
	case "xdotool":
		return d.exec(ctx, "xdotool", "key", "--", keys)
	case "ydotool":
		return errors.New("key combinations are not supported with ydotool yet; type the text instead")
	}
	// cliclick: hold the modifiers, type the last key, let go.
	parts := strings.Split(keys, "+")
	var argv []string
	mods := parts[:len(parts)-1]
	for _, m := range mods {
		argv = append(argv, "kd:"+strings.ToLower(m))
	}
	if last := parts[len(parts)-1]; len(last) == 1 {
		argv = append(argv, "t:"+last)
	} else {
		argv = append(argv, "kp:"+strings.ToLower(last)) // return, esc, tab …
	}
	for _, m := range mods {
		argv = append(argv, "ku:"+strings.ToLower(m))
	}
	return d.exec(ctx, append([]string{"cliclick"}, argv...)...)
}
