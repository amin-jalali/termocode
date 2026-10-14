package shotkit

import "time"

// EditorSteps opens three files from the demo workspace (see DemoWorkspace).
func EditorSteps() []Step {
	return []Step{
		Act("open:internal/toast/model.go"),
		Act("open:internal/theme/palette.go"),
		Act("open:internal/statusbar/model.go"),
		Pause(900 * time.Millisecond),
	}
}

// WithEditor is EditorSteps followed by extra.
func WithEditor(extra ...Step) []Step { return append(EditorSteps(), extra...) }

// TypeKeys sends one Key step per rune of s.
func TypeKeys(s string) []Step {
	var out []Step
	for _, r := range s {
		out = append(out, Key(string(r)))
	}
	return out
}

// Common scene steps, shared by the screenshot set and the golden tests.
var (
	paletteSteps = func() []Step {
		return WithEditor(append(append([]Step{Key("f1")}, TypeKeys("them")...), Pause(300*time.Millisecond))...)
	}
	scmSteps   = func() []Step { return WithEditor(Act("view:git"), Pause(1500*time.Millisecond)) }
	toastSteps = func() []Step {
		return WithEditor(Act("toast:Saved|internal/statusbar/model.go · 480 lines"), Pause(200*time.Millisecond))
	}
	confirmSteps  = func() []Step { return WithEditor(Act("palette:git-discard"), Pause(400*time.Millisecond)) }
	terminalSteps = func() []Step {
		return WithEditor(Act("palette:toggle-terminal"), Pause(1500*time.Millisecond),
			Act("termsend:ls --color=always -F"), Pause(1200*time.Millisecond))
	}
	aiEnv = map[string]string{"TERMOCODE_AI_PROVIDER": "ollama", "TERMOCODE_AI_MODEL": "qwen2.5-coder"}
)

// Common scenes by name.
func Dashboard() Scene {
	return Scene{Name: "dashboard", Steps: []Step{Pause(1500 * time.Millisecond)}}
}
func Editor() Scene   { return Scene{Name: "editor", Steps: EditorSteps()} }
func Palette() Scene  { return Scene{Name: "palette", Steps: paletteSteps()} }
func Toast() Scene    { return Scene{Name: "toast", Steps: toastSteps()} }
func SCM() Scene      { return Scene{Name: "scm", Steps: scmSteps()} }
func Confirm() Scene  { return Scene{Name: "confirm", Steps: confirmSteps()} }
func Terminal() Scene { return Scene{Name: "terminal", Steps: terminalSteps()} }

// AIChatAnswer is the AI chat panel with the canned demo conversation (no
// network).
func AIChatAnswer() Scene {
	return Scene{Name: "ai-chat-answer", Env: aiEnv, Steps: WithEditor(Act("palette:ai-open-chat"), Act("aidemo"), Pause(600*time.Millisecond))}
}

// MarkdownPreview opens docs/preview.md (a sample with every common block)
// in the full-screen Markdown preview.
func MarkdownPreview() Scene {
	return Scene{Name: "md-preview", Steps: []Step{Act("open:docs/preview.md"), Pause(600 * time.Millisecond), Act("palette:preview-md"), Pause(800 * time.Millisecond)}}
}

// Scenes is the screenshot set for assets/screenshots (the shots command).
func Scenes() []Scene {
	scenes := []Scene{
		Dashboard(), Editor(), Palette(), Toast(), SCM(),
		{Name: "scm-hover", Steps: []Step{Pause(800 * time.Millisecond), Act("view:git"), Pause(1500 * time.Millisecond),
			Hover(12, 20), Hover(12, 26), Hover(12, 32), Pause(500 * time.Millisecond)}},
		Confirm(),
		{Name: "ai-chat", Env: aiEnv, Steps: WithEditor(Act("palette:ai-open-chat"), Pause(600*time.Millisecond))},
		AIChatAnswer(),
		{Name: "problems", Steps: WithEditor(Act("palette:problems-panel"), Pause(800*time.Millisecond))},
		{Name: "testing", Steps: WithEditor(Act("palette:testing-focus"), Pause(1500*time.Millisecond))},
		{Name: "run-debug", Steps: WithEditor(Act("palette:debug-view"), Pause(800*time.Millisecond))},
		Terminal(),
		MarkdownPreview(),
		{Name: "md-preview-light", Theme: "catppuccin-latte", Steps: MarkdownPreview().Steps},
		{Name: "md-preview-ascii", Icons: "ascii", Steps: MarkdownPreview().Steps},
		{Name: "image-preview", Steps: WithEditor(Act("openfile:assets/demo.png"), Pause(800*time.Millisecond))},
		{Name: "quick-open", Steps: WithEditor(Key("ctrl+p"), Key("m"), Key("o"), Key("d"), Pause(800*time.Millisecond))},
	}
	for _, th := range []string{"catppuccin-mocha", "catppuccin-latte", "tokyo-night", "rose-pine", "rose-pine-dawn"} {
		scenes = append(scenes, Scene{Name: "theme-" + th, Theme: th, Steps: EditorSteps()})
		scenes = append(scenes, Scene{Name: "theme-" + th + "-palette", Theme: th, Steps: paletteSteps()})
	}
	scenes = append(scenes,
		Scene{Name: "theme-catppuccin-latte-dashboard", Theme: "catppuccin-latte", Steps: []Step{Pause(1500 * time.Millisecond)}},
		Scene{Name: "theme-catppuccin-latte-scm", Theme: "catppuccin-latte", Steps: scmSteps()},
	)
	for _, mode := range []string{"unicode", "ascii"} {
		scenes = append(scenes, Scene{Name: "icons-" + mode, Icons: mode, Steps: EditorSteps()})
	}
	return scenes
}
