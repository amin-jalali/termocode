package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/cfgfile"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Image generation for the agent (CMP-97). generate_image asks the main
// model slot's OpenAI-compatible images endpoint for one PNG (an icon, a
// placeholder, an illustration for the docs) and saves it as a NEW file
// in the workspace. It is off by default (config.json ai_image_gen; AI:
// Toggle Image Generation) and each call costs money, so every call asks
// first, with no "Always allow" (Mutating with Class ""). Only the user
// turns it on; the AI cannot (ai_actions_policy.go).

// aiImageSettings are the image keys of config.json.
type aiImageSettings struct {
	On    bool   `json:"ai_image_gen,omitempty"`
	Model string `json:"ai_image_model,omitempty"` // "": ai.DefaultImageModel
}

func loadAIImageSettings() aiImageSettings {
	var s aiImageSettings
	if path, err := ai.ConfigPath(); err == nil {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &s)
		}
	}
	return s
}

// saveAIImageSettings writes them back, keeping every other key.
func saveAIImageSettings(s aiImageSettings) error {
	path, err := ai.ConfigPath()
	if err != nil {
		return err
	}
	return cfgfile.Update(path, func(m map[string]any) error {
		if s.On {
			m["ai_image_gen"] = true
		} else {
			delete(m, "ai_image_gen")
		}
		if s.Model == "" {
			delete(m, "ai_image_model")
		} else {
			m["ai_image_model"] = s.Model
		}
		return nil
	})
}

// aiToggleImageGen is AI: Toggle Image Generation.
func (m *Model) aiToggleImageGen() tea.Cmd {
	s := loadAIImageSettings()
	s.On = !s.On
	if err := saveAIImageSettings(s); err != nil {
		return m.aiToastErr("Settings error", err)
	}
	label := "AI image generation off"
	if s.On {
		label = "AI image generation on: the agent may ask to create PNG files (each one is a paid request and asks first)"
		if m.ai != nil {
			if sl, err := m.ai.resolveSlot(""); err != nil || !ai.ImageGenSupported(sl.cfg.Kind) {
				label = "AI image generation on, but the main model's provider has no images API (use OpenAI or an OpenAI-compatible endpoint)"
			}
		}
	}
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, label)
	return c
}

// aiImagePath checks the path argument: a new .png inside the workspace.
func aiImagePath(ws ai.Workspace, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("path is required")
	}
	if !strings.EqualFold(filepath.Ext(p), ".png") {
		return "", errors.New("path must end in .png")
	}
	abs, err := ws.Resolve(p)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(abs); err == nil {
		return "", fmt.Errorf("%s already exists; pick a new file name (generate_image never overwrites)", p)
	}
	return abs, nil
}

// aiImageTools is generate_image when it is on and the main slot can
// make images; nil otherwise.
func (m *Model) aiImageTools(root string, call aiIDECaller) []ai.Tool {
	set := loadAIImageSettings()
	if !set.On || m.ai == nil {
		return nil
	}
	sl, err := m.ai.resolveSlot("")
	if err != nil || !ai.ImageGenSupported(sl.cfg.Kind) {
		return nil
	}
	cfg := sl.cfg
	model := set.Model
	if model == "" {
		model = ai.DefaultImageModel
	}
	ws := ai.Workspace{Root: root}
	rel := aiRel(root)
	return []ai.Tool{{
		Name: "generate_image",
		Description: "Generate one PNG image from a text prompt (an icon, a placeholder, an illustration) and save it as a NEW file in the workspace. " +
			"Each call is a paid request and the user confirms it. Never overwrites a file.",
		Params: map[string]any{
			"prompt": map[string]any{"type": "string", "description": "what the image shows, in detail (style, colors, background)"},
			"path":   map[string]any{"type": "string", "description": "new file path in the workspace, ending in .png"},
			"size":   map[string]any{"type": "string", "description": "one of " + strings.Join(ai.ImageSizes, ", ") + " (default auto)"},
		},
		Required: []string{"prompt", "path"},
		Mutating: true, // Class "": every call asks, no Always allow
		Describe: func(a map[string]any) string {
			pr := []rune(strings.TrimSpace(ai.ArgString(a, "prompt")))
			if len(pr) > 80 {
				pr = append(pr[:80], '…')
			}
			return fmt.Sprintf("Generate image %s (paid request to %s): %s", ai.ArgString(a, "path"), model, string(pr))
		},
		Validate: func(a map[string]any) error {
			if strings.TrimSpace(ai.ArgString(a, "prompt")) == "" {
				return errors.New("prompt is required")
			}
			if sz := ai.ArgString(a, "size"); sz != "" && !slices.Contains(ai.ImageSizes, sz) {
				return fmt.Errorf("size must be one of %s", strings.Join(ai.ImageSizes, ", "))
			}
			_, err := aiImagePath(ws, ai.ArgString(a, "path"))
			return err
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			abs, err := aiImagePath(ws, ai.ArgString(a, "path"))
			if err != nil {
				return "", err
			}
			img, err := ai.GenerateImage(ctx, cfg, ai.ImageRequest{Prompt: ai.ArgString(a, "prompt"), Model: model, Size: ai.ArgString(a, "size")})
			if err != nil {
				return "", err
			}
			// R3: AI: Undo Last Turn deletes the new file again
			_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) { m.aiRestoreNote(abs); return "", nil, nil })
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return "", err
			}
			f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if err != nil {
				return "", err
			}
			if _, err := f.Write(img); err != nil {
				_ = f.Close()
				_ = os.Remove(abs)
				return "", err
			}
			if err := f.Close(); err != nil {
				return "", err
			}
			note, _ := call(ctx, func(m *Model) (string, tea.Cmd, error) {
				n, cmd := m.aiSyncIDE(abs, aiSyncDisk)
				return n, cmd, nil
			})
			return fmt.Sprintf("saved %s (%d KB); open it for the user with open_file when they want to see it", rel(abs), (len(img)+1023)/1024) + note, nil
		},
	}}
}
