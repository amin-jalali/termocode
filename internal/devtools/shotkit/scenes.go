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

// DashboardRecent is the start page of a returning user: recent files and
// two workspaces.
func DashboardRecent() Scene {
	return Scene{Name: "dashboard-recent", Steps: []Step{Pause(1500 * time.Millisecond)},
		Recents: []string{"internal/statusbar/model.go", "internal/theme/palette.go", "internal/toast/model.go",
			"docs/preview.md", "go.mod", "README.md", "internal/app/view.go"},
		Workspaces: []string{".", "../play/httpd"}}
}

func Editor() Scene   { return Scene{Name: "editor", Steps: EditorSteps()} }
func Palette() Scene  { return Scene{Name: "palette", Steps: paletteSteps()} }
func Toast() Scene    { return Scene{Name: "toast", Steps: toastSteps()} }
func SCM() Scene      { return Scene{Name: "scm", Steps: scmSteps()} }
func Confirm() Scene  { return Scene{Name: "confirm", Steps: confirmSteps()} }
func Terminal() Scene { return Scene{Name: "terminal", Steps: terminalSteps()} }

// SCMCommit is the commit view: HEAD (the demo merge) in the diff card,
// docked over the editor when it is wide enough, floating otherwise.
func SCMCommit() Scene {
	return Scene{Name: "scm-commit", Steps: []Step{Act("view:git"), Pause(1500 * time.Millisecond),
		Act("commit:HEAD"), Pause(1200 * time.Millisecond)}}
}

// AIChatAnswer is the AI chat panel with the canned demo conversation (no
// network).
func AIChatAnswer() Scene {
	return Scene{Name: "ai-chat-answer", Env: aiEnv, Steps: WithEditor(Act("palette:ai-open-chat"), Act("aidemo"), Pause(600*time.Millisecond))}
}

// MarkdownPreview opens docs/preview.md (a sample with every common block)
// in the full-screen Markdown preview.
// FindResults is the Find Results page for "Toast" (Find in Files).
func FindResults() Scene {
	steps := append([]Step{Act("palette:find-files")}, TypeKeys("Toast")...)
	return Scene{Name: "find-results", Steps: WithEditor(append(steps, Key("enter"), Pause(1500*time.Millisecond))...)}
}

func MarkdownPreview() Scene {
	return Scene{Name: "md-preview", Steps: []Step{Act("open:docs/preview.md"), Pause(600 * time.Millisecond), Act("palette:preview-md"), Pause(800 * time.Millisecond)}}
}

// DiffReview is the AI review card (Group R) with two sample files, the
// second change focused and the first accepted. Nothing is written.
func DiffReview() Scene {
	return Scene{Name: "diff-review", Theme: "tokyo-night", Steps: WithEditor(Act("diffdemo"), Pause(600*time.Millisecond))}
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
		{Name: "ai-chat-fa", Env: aiEnv, Steps: WithEditor(Act("palette:ai-open-chat"), Act("aidemo-fa"), Pause(600*time.Millisecond))},
		{Name: "problems", Steps: WithEditor(Act("palette:problems-panel"), Pause(800*time.Millisecond))},
		{Name: "testing", Steps: WithEditor(Act("palette:testing-focus"), Pause(1500*time.Millisecond))},
		{Name: "run-debug", Steps: WithEditor(Act("palette:debug-view"), Pause(800*time.Millisecond))},
		Terminal(),
		MarkdownPreview(),
		DiffReview(),
		{Name: "diff-review-light", Theme: "catppuccin-latte", Steps: DiffReview().Steps},
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

// DemoScene is the README demo (shots -gif): start page, Quick Open, real
// Neovim motions, the command palette, an AI answer, the AI change review
// and the terminal. Holds set how long each frame shows in the GIF.
func DemoScene() Scene {
	hold := func(st Step, d time.Duration) Step { st.Hold = d; return st }
	typed := func(s string) []Step {
		var out []Step
		for _, st := range TypeKeys(s) {
			out = append(out, hold(st, 110*time.Millisecond))
		}
		return out
	}
	steps := []Step{hold(Pause(200*time.Millisecond), 1800*time.Millisecond)}
	// Quick Open a file by typing part of its path.
	steps = append(steps, hold(Key("ctrl+p"), 500*time.Millisecond))
	steps = append(steps, typed("statusbar/model")...)
	steps = append(steps, hold(Pause(300*time.Millisecond), 900*time.Millisecond),
		hold(Key("enter"), 1600*time.Millisecond))
	// Real Neovim: leave insert mode, move by lines and words, select lines.
	steps = append(steps, hold(Key("esc"), 400*time.Millisecond))
	for _, k := range []string{"j", "j", "j", "w", "w"} {
		steps = append(steps, hold(Key(k), 220*time.Millisecond))
	}
	steps = append(steps, hold(Key("V"), 300*time.Millisecond), hold(Key("j"), 300*time.Millisecond),
		hold(Key("j"), 1200*time.Millisecond), hold(Key("esc"), 400*time.Millisecond))
	// Every action in the palette.
	steps = append(steps, hold(Key("f1"), 500*time.Millisecond))
	steps = append(steps, typed("theme")...)
	steps = append(steps, hold(Pause(200*time.Millisecond), 1400*time.Millisecond), hold(Key("esc"), 400*time.Millisecond))
	// AI chat next to the editor, then its change as a reviewable diff.
	steps = append(steps,
		hold(Act("palette:ai-open-chat"), 800*time.Millisecond),
		hold(Act("aidemo"), 3500*time.Millisecond),
		hold(Act("diffdemo"), 3500*time.Millisecond),
		hold(Key("esc"), 500*time.Millisecond),
		// The integrated terminal.
		hold(Act("palette:toggle-terminal"), 800*time.Millisecond),
		hold(Act("termsend:ls --color=always -F"), 2500*time.Millisecond),
	)
	return Scene{Name: "demo", Env: aiEnv, Steps: steps}
}
