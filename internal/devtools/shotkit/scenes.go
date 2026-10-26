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

// TerminalMarks is the terminal tab bar with a CLI agent waiting for the
// user (first tab) and the "✦ Fix?" chip of a failed command (U8, U9b).
func TerminalMarks() Scene {
	return Scene{Name: "terminal-marks", Env: aiEnv, Steps: WithEditor(Act("palette:toggle-terminal"), Pause(1500*time.Millisecond),
		Act("palette:terminal-new-tab"), Pause(1000*time.Millisecond), Act("termdemo"), Pause(300*time.Millisecond))}
}

// SCMCommit is the commit view: HEAD (the demo merge) in the diff card,
// docked over the editor when it is wide enough, floating otherwise.
func SCMCommit() Scene {
	return Scene{Name: "scm-commit", Steps: []Step{Act("view:git"), Pause(1500 * time.Millisecond),
		Act("commit:HEAD"), Pause(1200 * time.Millisecond)}}
}

// SCMCompare compares the oldest demo commit with HEAD (Space on both
// GRAPH rows).
func SCMCompare() Scene {
	return Scene{Name: "scm-compare", Steps: []Step{Act("view:git"), Pause(1500 * time.Millisecond),
		Act("compare:3,0"), Pause(1200 * time.Millisecond)}}
}

// Services is the Services view with the canned demo snapshot (no
// docker): the editor on the right, postgres under the cursor.
func Services() Scene {
	return Scene{Name: "services", Steps: WithEditor(Act("svcdemo"), Pause(400*time.Millisecond))}
}

// ServicesVariant is Services with the "down" or "empty" snapshot.
func ServicesVariant(name, variant string) Scene {
	return Scene{Name: name, Steps: WithEditor(Act("svcdemo:"+variant), Pause(400*time.Millisecond))}
}

func Containers() Scene {
	return Scene{Name: "containers", Steps: WithEditor(Act("contdemo"), Pause(400*time.Millisecond))}
}

func Databases() Scene {
	return Scene{Name: "db-tree", Steps: WithEditor(Act("dbdemo"), Pause(400*time.Millisecond))}
}
func DatabasesVariant(name, variant string) Scene {
	return Scene{Name: name, Steps: WithEditor(Act("dbdemo:"+variant), Pause(400*time.Millisecond))}
}

// dbCardScenes are the Databases overlays and states of the DB-16 set
// (app databaseCardsDemo): each runs at every golden size.
func dbCardScenes() []Scene {
	return []Scene{
		DatabasesVariant("db-console", "console"),
		DatabasesVariant("db-add-picker", "add-picker"),
		// Enter on PostgreSQL opens the first step of the form.
		DatabasesVariant("db-tx-card", "tx-card"),
		DatabasesVariant("db-disconnect-card", "disconnect-card"),
		// ? on the tree: the key card (X-1).
		DatabasesVariant("db-keys-card", "keys"),
		// "/" and "us": the inline filter (X-2).
		DatabasesVariant("db-filter", "filter"),
		DatabasesVariant("db-redis-delete-card", "redis-delete"),
		DatabasesVariant("db-password-move-card", "password-move"),
		DatabasesVariant("db-ai-card", "ai-card"),
		DatabasesVariant("db-connecting", "connecting"),
		{Name: "db-add-form", Steps: WithEditor(Act("dbdemo:add-picker"), Key("enter"), Pause(400*time.Millisecond))},
	}
}

func ContainersVariant(name, variant string) Scene {
	return Scene{Name: name, Steps: WithEditor(Act("contdemo:"+variant), Pause(400*time.Millisecond))}
}

func ContainersRemoveConfirm() Scene {
	return Scene{Name: "containers-remove-confirm", Steps: WithEditor(Act("contdemo:remove"), Key("D"), Pause(400*time.Millisecond))}
}

func ContainersStopRemoveConfirm() Scene {
	return Scene{Name: "containers-stop-remove-confirm", Steps: WithEditor(Act("contdemo:stop-remove"), Key("D"), Pause(400*time.Millisecond))}
}

func ContainersComposeDownConfirm() Scene {
	return Scene{Name: "containers-compose-down-confirm", Steps: WithEditor(Act("contdemo:compose-down"), Key("D"), Pause(400*time.Millisecond))}
}

// SecretsScene shows the secret store: variant "card" (migration card),
// "locked" or "open" (the store screen, see app secretsdemo).
func SecretsScene(name, variant string) Scene {
	return Scene{Name: name, Steps: WithEditor(Act("secretsdemo:"+variant), Pause(600*time.Millisecond))}
}

// MCPServeScene shows termocode's MCP server: variant "status" (the
// status screen), "card" (an outside agent's confirm card) or "chip" (the
// status bar with the server listening and no agent) or "opencode"
// (OpenCode's "add termocode" card) or "tip-gemini" / "tip-aider" (the hand
// tip toast), "allow" (the Agent Auto-Approve list) or "idle" / "off" (the
// status screen while nothing listens), see app mcpservedemo.
func MCPServeScene(name, variant string) Scene {
	return Scene{Name: name, Steps: WithEditor(Act("mcpservedemo:"+variant), Pause(600*time.Millisecond))}
}

func ContainersImages() Scene   { return ContainersVariant("containers-images", "images") }
func ContainersNetworks() Scene { return ContainersVariant("containers-networks", "networks") }
func ContainersVolumes() Scene  { return ContainersVariant("containers-volumes", "volumes") }
func ContainersNetworkUsers() Scene {
	return ContainersVariant("containers-network-users", "network-users")
}
func ContainersHostCard(name, variant string) Scene { return ContainersVariant(name, variant) }

// AIChatAnswer is the AI chat panel with the canned demo conversation (no
// network).
func AIChatAnswer() Scene {
	return Scene{Name: "ai-chat-answer", Env: aiEnv, Steps: WithEditor(Act("palette:ai-open-chat"), Act("aidemo"), Pause(600*time.Millisecond))}
}

// AIPipeline is a two-step agent pipeline run (the built-in "review"
// profile on scripted models, no network): "running" while the second
// step writes, "done" with the folded cards and the run row (U20 AP-4).
func AIPipeline(variant string) Scene {
	return Scene{Name: "ai-pipeline-" + variant, Env: aiEnv,
		Steps: WithEditor(Act("palette:ai-open-chat"), Act("aipipedemo:"+variant), Pause(900*time.Millisecond))}
}

// AIUntilDone is a work-until-done run (OM-3) of the built-in
// until-done profile on scripted models: "done" passes in round 2 after
// one fix, "fail" still fails after the last round.
func AIUntilDone(variant string) Scene {
	return Scene{Name: "ai-until-done-" + variant, Env: aiEnv,
		Steps: WithEditor(Act("palette:ai-open-chat"), Act("untildonedemo:"+variant), Pause(1200*time.Millisecond))}
}

// AgentsPanel is the AGENTS view (U20 AP-9) of a planned diamond run on
// scripted models: "running" with its two middle steps working,
// "done" when the run finished.
func AgentsPanel(variant string) Scene {
	return Scene{Name: "agents-panel-" + variant, Env: aiEnv,
		Steps: WithEditor(Act("palette:ai-open-chat"), Act("agentsdemo:"+variant), Pause(1200*time.Millisecond))}
}

// AIUsage is the AI: Usage screen (AU-4) after the planned diamond run
// of AgentsPanel("done"): this run and this session, by model and by
// agent (scripted models, an earlier run is in the session).
func AIUsage() Scene {
	return Scene{Name: "ai-usage", Env: aiEnv,
		Steps: WithEditor(Act("palette:ai-open-chat"), Act("usagedemo:done"), Pause(1200*time.Millisecond))}
}

// AIProfile is the agent profile UI (U20 AP-5) with sample data:
// "chip" the chat header chip "✦ review ▾" (and the status badge),
// "picker" the profile picker with an untrusted project profile,
// "trust" that project profile's trust card, "save" the save preview,
// "show" the read-only AI: Show Profile card.
func AIProfile(variant string) Scene {
	return Scene{Name: "ai-profile-" + variant, Env: aiEnv,
		Steps: WithEditor(Act("palette:ai-open-chat"), Act("profiledemo:"+variant), Pause(600*time.Millisecond))}
}

// Runs is the Runs view and page (sample runs, no model, nothing
// written): "list" the list and the overview, "detail" the live run's
// page, "stall" its page while it is stalled, "empty" no run at all.
func Runs(variant string) Scene {
	return Scene{Name: "runs-" + variant, Env: aiEnv,
		Steps: WithEditor(Act("runsdemo:"+variant), Pause(900*time.Millisecond))}
}

// AIMemory is the AI memory UI (OM-2) with sample lessons: "chat" a turn
// that saved lessons, "ask" the ask-once card, "picker" AI: Memory…,
// "forget" a lesson's Forget card.
func AIMemory(variant string) Scene {
	return Scene{Name: "ai-memory-" + variant, Env: aiEnv,
		Steps: WithEditor(Act("palette:ai-open-chat"), Act("memorydemo:"+variant), Pause(600*time.Millisecond))}
}

// AISkills is the AI skills UI (OM-4) with sample skills: "chat" a turn
// that loaded a skill, "picker" AI: Skills…, "trust" the project skills
// trust card.
func AISkills(variant string) Scene {
	return Scene{Name: "ai-skills-" + variant, Env: aiEnv,
		Steps: WithEditor(Act("palette:ai-open-chat"), Act("skillsdemo:"+variant), Pause(600*time.Millisecond))}
}

// Itermoc is the Itermoc sidebar view (AI profiles, models, services and
// outside agents) with sample data: "list" a working setup with one row
// that needs the user in each section (the list only, no detail page),
// "follow" the keys o, Esc and j: the page tab follows the list, "profile" the same with the detail page of the profile review open in
// the editor area, "service" the page of a service whose sign-in ended,
// "agent" the page of a connected outside agent, "agents" the page of the
// listening row, "overview" the overview page (tiles, what needs you, the
// active flow),
// "empty" a fresh install, "menu" the
// actions menu of the first profile row, "slotadd" the Add slot form,
// "slotkey" the Set key offer after a new slot, "slotremove" the Remove
// card of a slot a profile uses; "ai-switch", "ai-draft", "ai-slot",
// "ai-connect", "ai-access" and "ai-start" the confirm card of each AI
// write tool (AH-8).
func Itermoc(variant string) Scene {
	name := "itermoc-" + variant
	demo := variant
	if variant == "list" || variant == "follow" {
		demo = "full"
	}
	steps := WithEditor(Act("itermocdemo:"+demo), Pause(600*time.Millisecond))
	if variant == "follow" {
		// o opens the page of the second profile (and moves the keys to it);
		// Esc gives them back to the list, where j moves to the next profile
		// and the same tab follows.
		steps = WithEditor(Act("itermocdemo:full"), Pause(300*time.Millisecond), Key("j"), Key("j"), Key("o"), Pause(500*time.Millisecond),
			Key("esc"), Key("j"), Pause(500*time.Millisecond))
	}
	return Scene{Name: name, Env: aiEnv, Steps: steps}
}

// MarkdownPreview opens docs/preview.md (a sample with every common block)
// in the full-screen Markdown preview.
// FindResults is the Find Results page for "Toast" (Find in Files).
func FindResults() Scene {
	steps := append([]Step{Act("palette:find-files")}, TypeKeys("Toast")...)
	return Scene{Name: "find-results", Steps: WithEditor(append(steps, Key("enter"), Pause(1500*time.Millisecond))...)}
}

// HTTPFile is an .http file with the request lenses (no network).
func HTTPFile() Scene {
	return Scene{Name: "http-file", Steps: []Step{Act("httpdemo:file"), Pause(900 * time.Millisecond)}}
}

// HTTPResponse is the HTTP Response page with a canned JSON answer.
func HTTPResponse() Scene {
	return Scene{Name: "http-response", Steps: []Step{Act("httpdemo:response"), Pause(900 * time.Millisecond)}}
}

// HTTPResponseTLS is the page's TLS error state.
func HTTPResponseTLS() Scene {
	return Scene{Name: "http-response-tls", Steps: []Step{Act("httpdemo:tls"), Pause(900 * time.Millisecond)}}
}

func MarkdownPreview() Scene {
	return Scene{Name: "md-preview", Steps: []Step{Act("open:docs/preview.md"), Pause(600 * time.Millisecond), Act("palette:preview-md"), Pause(800 * time.Millisecond)}}
}

// DiffReview is the AI review card (Group R) with two sample files, the
// second change focused and the first accepted. Nothing is written.
func DiffReview() Scene {
	return Scene{Name: "diff-review", Theme: "tokyo-night", Steps: WithEditor(Act("diffdemo"), Pause(600*time.Millisecond))}
}

// HelpTooltip is the hover label of the status bar's git branch with its
// "More info" line, the pointer resting on that line (it lights up).
func HelpTooltip() Scene {
	return Scene{Name: "help-tooltip", KeepHover: true, Steps: WithEditor(HoverLastText("main"), Pause(900*time.Millisecond),
		HoverText("More info"), Pause(700*time.Millisecond))}
}

// HelpViewer is the in-app guide at the Chat panel section.
func HelpViewer() Scene {
	return Scene{Name: "help-viewer", Steps: WithEditor(Act("helpdemo"), Pause(700*time.Millisecond))}
}

// WelcomeHover is the START tile hover label on the welcome dashboard.
func WelcomeHover() Scene {
	return Scene{Name: "welcome-hover", KeepHover: true, Steps: []Step{HoverText("Open file"), Pause(900 * time.Millisecond)}}
}

// ExplorerHover is the fixed-kind hover label on a folder row. No file is
// opened or revealed first: that loads async and made the frame flaky.
func ExplorerHover() Scene {
	return Scene{Name: "explorer-hover", KeepHover: true, Steps: []Step{Act("view:files"), Pause(900 * time.Millisecond), HoverText("assets"), Pause(900 * time.Millisecond)}}
}

// Scenes is the screenshot set for assets/screenshots (the shots command).
func Scenes() []Scene {
	scenes := []Scene{
		withName(DashboardRecent(), "dashboard"), Editor(), Palette(), Toast(), SCM(),
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
		withTheme(withName(DashboardRecent(), "theme-catppuccin-latte-dashboard"), "catppuccin-latte"),
		Scene{Name: "theme-catppuccin-latte-scm", Theme: "catppuccin-latte", Steps: scmSteps()},
	)
	for _, mode := range []string{"unicode", "ascii"} {
		scenes = append(scenes, Scene{Name: "icons-" + mode, Icons: mode, Steps: EditorSteps()})
	}
	return scenes
}

func withName(sc Scene, name string) Scene   { sc.Name = name; return sc }
func withTheme(sc Scene, theme string) Scene { sc.Theme = theme; return sc }

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

// ImportPicker is Preferences: Import from Another Editor… with three
// sample editors in the sandbox config dir.
func ImportPicker() Scene {
	return Scene{Name: "import-picker", Steps: WithEditor(Act("importdemo:picker"), Pause(900*time.Millisecond))}
}

// ImportPanel is the review panel of the VS Code sample: settings, keys,
// theme and extensions, the user's current values shown as old → new.
func ImportPanel() Scene {
	return Scene{Name: "import-panel", Steps: WithEditor(Act("importdemo:panel"), Pause(600*time.Millisecond))}
}

// ImportPickerEmpty is the picker when no other editor is found: only
// "From a file…" and a note.
func ImportPickerEmpty() Scene {
	return Scene{Name: "import-picker-empty", Steps: WithEditor(Act("importdemo:empty"), Pause(900*time.Millisecond))}
}

// ImportFilePrompt is the path prompt behind "From a file…".
func ImportFilePrompt() Scene {
	return Scene{Name: "import-file-prompt", Steps: WithEditor(Act("importdemo:file"), Pause(600*time.Millisecond))}
}

// ImportConfirm is the final card of the VS Code sample: what will change
// in plain words, the files in the last line, Import and Cancel.
func ImportConfirm() Scene {
	return Scene{Name: "import-confirm", Steps: WithEditor(Act("importdemo:confirm"), Pause(600*time.Millisecond))}
}

// ImportUndo is the Undo card after the VS Code sample was imported:
// Cancel first and focused, Undo import marked as the risky button.
func ImportUndo() Scene {
	return Scene{Name: "import-undo", Steps: WithEditor(Act("importdemo:undo"), Pause(600*time.Millisecond))}
}

// ImportSetupRow is the first-run setup card after it found the sample
// editors: the optional Import row with its button.
func ImportSetupRow() Scene {
	return Scene{Name: "setup-card-import", SetupCard: true, Env: map[string]string{"TERM": "xterm-kitty"},
		Steps: []Step{Pause(1500 * time.Millisecond), Act("importdemo:setup"), Pause(1200 * time.Millisecond)}}
}

// ImportSnippets is the review panel with sample snippets, scrolled to the
// Snippets section with the clashing snippet focused.
func ImportSnippets() Scene {
	return Scene{Name: "import-snippets", Steps: WithEditor(Act("importdemo:snippets"),
		Key("]"), Key("]"), Key("]"), Key("down"), Pause(600*time.Millisecond))}
}
