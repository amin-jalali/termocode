package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/computeruse"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// Computer use (CMP-90, docs/adr/0024-computer-use.md): with
// ai_computer_use on (config.json, by hand; off by default) the chat
// gets four desktop tools. Each one is mutating with no class, so every
// call shows a card with no "Always allow", and ai.ClassifyRisk rates
// it high, so autopilot and allow rules never skip that card.

// aiScreensDir is where screenshots go, inside the workspace so look_at
// can read them.
const aiScreensDir = ".termocode/screens"

// aiComputerDriver finds the desktop driver (a seam for tests).
var aiComputerDriver = func() (computeruse.Driver, error) {
	return computeruse.Detect(computeruse.Env{
		OS:       runtime.GOOS,
		Getenv:   os.Getenv,
		LookPath: exec.LookPath,
		Run: func(ctx context.Context, name string, args ...string) error {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			out, err := spawn.CommandContext(ctx, name, args...).CombinedOutput()
			if err != nil {
				if s := strings.TrimSpace(string(out)); s != "" {
					return fmt.Errorf("%w: %s", err, aiCut(s, 300))
				}
				return err
			}
			return nil
		},
	})
}

func aiCut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// aiComputerOn reports ai_computer_use.
func aiComputerOn() bool {
	p, err := ai.ConfigPath()
	if err != nil {
		return false
	}
	return ai.LoadSettings(p).ComputerUseOn()
}

// aiComputerTools are the desktop tools, or none while the setting is off.
func aiComputerTools(root string) []ai.Tool {
	if !aiComputerOn() {
		return nil
	}
	driver := func() (computeruse.Driver, error) {
		d, err := aiComputerDriver()
		if err != nil {
			return nil, err
		}
		if d == nil {
			return nil, errors.New("no desktop driver")
		}
		return d, nil
	}
	num := func(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	point := func(a map[string]any) (int, int) { return ai.ArgInt(a, "x"), ai.ArgInt(a, "y") }
	button := func(a map[string]any) string {
		if b := ai.ArgString(a, "button"); b != "" {
			return b
		}
		return "left"
	}
	return []ai.Tool{
		{
			Name: "computer_screenshot",
			Description: "Take a screenshot of the whole desktop and save it as a PNG in the workspace (" + aiScreensDir + "). " +
				"Then use look_at on that file to see it. The user confirms each screenshot.",
			Params:   map[string]any{},
			Mutating: true,
			Describe: func(map[string]any) string { return "Take a screenshot of the desktop" },
			Run: func(ctx context.Context, _ map[string]any) (string, error) {
				d, err := driver()
				if err != nil {
					return "", err
				}
				dir := filepath.Join(root, filepath.FromSlash(aiScreensDir))
				if err := os.MkdirAll(dir, 0o700); err != nil {
					return "", err
				}
				name := "screen-" + time.Now().Format("20060102-150405.000") + ".png"
				if err := d.Screenshot(ctx, filepath.Join(dir, name)); err != nil {
					return "", err
				}
				rel := aiScreensDir + "/" + name
				return "Saved " + rel + " (" + d.Name() + "). Use look_at with path " + rel + " to see it.", nil
			},
		},
		{
			Name:        "computer_click",
			Description: "Click the mouse at a point of the desktop (pixels from the top left). The user confirms each click.",
			Params: map[string]any{
				"x":      num("pixels from the left"),
				"y":      num("pixels from the top"),
				"button": map[string]any{"type": "string", "enum": computeruse.Buttons, "description": "left (default), middle or right"},
			},
			Required: []string{"x", "y"},
			Mutating: true,
			Validate: func(a map[string]any) error {
				x, y := point(a)
				if err := computeruse.CheckPoint(x, y); err != nil {
					return err
				}
				return computeruse.CheckButton(button(a))
			},
			Describe: func(a map[string]any) string {
				x, y := point(a)
				return fmt.Sprintf("Click %s at %d, %d on the desktop", button(a), x, y)
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				d, err := driver()
				if err != nil {
					return "", err
				}
				x, y := point(a)
				if err := d.Click(ctx, x, y, button(a)); err != nil {
					return "", err
				}
				return fmt.Sprintf("Clicked %s at %d, %d.", button(a), x, y), nil
			},
		},
		{
			Name:        "computer_type",
			Description: "Type text into the focused desktop window, as if from the keyboard. The user confirms each call and sees the text.",
			Params:      map[string]any{"text": str("the text to type")},
			Required:    []string{"text"},
			Mutating:    true,
			Validate:    func(a map[string]any) error { return computeruse.CheckText(ai.ArgString(a, "text")) },
			Describe:    func(a map[string]any) string { return "Type on the desktop: " + ai.Redact(ai.ArgString(a, "text")) },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				d, err := driver()
				if err != nil {
					return "", err
				}
				if err := d.Type(ctx, ai.ArgString(a, "text")); err != nil {
					return "", err
				}
				return "Typed it.", nil
			},
		},
		{
			Name:        "computer_key",
			Description: "Press a key or key combination in the focused desktop window (like ctrl+s, Return, alt+Tab). The user confirms each call.",
			Params:      map[string]any{"keys": str("key names joined by +")},
			Required:    []string{"keys"},
			Mutating:    true,
			Validate:    func(a map[string]any) error { return computeruse.CheckKeys(ai.ArgString(a, "keys")) },
			Describe:    func(a map[string]any) string { return "Press " + ai.ArgString(a, "keys") + " on the desktop" },
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				d, err := driver()
				if err != nil {
					return "", err
				}
				if err := d.Key(ctx, ai.ArgString(a, "keys")); err != nil {
					return "", err
				}
				return "Pressed " + ai.ArgString(a, "keys") + ".", nil
			},
		},
	}
}
