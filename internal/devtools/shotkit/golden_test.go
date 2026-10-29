package shotkit

// Golden screen tests: each case renders one scene at a fixed size and
// compares the frame (text with ANSI colours) with testdata/golden/<name>.ans.
//
//	go test ./internal/devtools/shotkit                 # compare
//	go test ./internal/devtools/shotkit -update         # accept new frames
//	UPDATE_GOLDEN=1 go test ./internal/devtools/shotkit # same
//	go test ./internal/devtools/shotkit -run 'Golden/scm'
//
// Every case runs in its own child process (the test binary re-executed,
// see TestMain) with a private HOME, XDG dirs, workspace copy and a PATH that
// only holds nvim, git, sh and ls, so cases can run in parallel and nothing
// from the developer's machine (config, plugins, language servers, rg)
// leaks into the frame. The clock is frozen (internal/clock), git dates are
// fixed, animations are off and cursor blinking is dropped.

import (
	"context"
	"flag"
	"fmt"
	"image/color"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/devtools/ansipng"
	"github.com/amin-jalali/termocode/internal/theme"
)

var update = flag.Bool("update", false, "rewrite the golden frames in testdata/golden")

const (
	childCaseEnv = "SHOTKIT_GOLDEN_CASE" // set in the child: which case to render
	childOutEnv  = "SHOTKIT_GOLDEN_OUT"  // set in the child: where to write the frame
	goldenDir    = "testdata/golden"
)

type goldenCase struct {
	name  string
	scene Scene
	w, h  int
}

// goldenScenes are the screens under test. Each runs at every size in
// goldenSizes; goldenExtras run at the first size only.
func goldenScenes() []Scene {
	return append([]Scene{
		Dashboard(),
		Editor(),
		Palette(),
		SCM(),
		{Name: "scm-hover", Steps: append(scmStepsNoEditor(), HoverText("Merge branc"), Pause(600*time.Millisecond))},
		SCMCommit(),
		Terminal(),
		TerminalMarks(),
		{Name: "ai-chat", Env: aiEnv, Steps: WithEditor(Act("palette:ai-open-chat"), Act("aidemo"), Pause(600*time.Millisecond))},
		{Name: "settings", Steps: WithEditor(Act("palette:open-settings-ui"), Pause(600*time.Millisecond))},
		Confirm(),
		Toast(),
		{Name: "explorer", Steps: []Step{Act("view:files"), Act("open:internal/theme/palette.go"), Act("palette:reveal-file"), Pause(600 * time.Millisecond)}},
		FindResults(),
		{Name: "search", Steps: WithEditor(append(append([]Step{Act("palette:find-files-overlay")}, TypeKeys("Toast")...), Pause(1200*time.Millisecond))...)},
		Services(),
		Databases(),
		DatabasesVariant("db-empty", "empty"),
		DatabasesVariant("db-no-sqlite", "no-sqlite"),
		DatabasesVariant("db-stopped-hint", "stopped"),
		ContainersVariant("containers-db-service-menu", "db-menu"),
		ContainersVariant("containers-db-container-menu", "db-container-menu"),
		DatabasesVariant("db-result", "result"),
		DatabasesVariant("db-messages", "messages"),
		DatabasesVariant("db-error", "error"),
		DatabasesVariant("db-table", "table"),
		DatabasesVariant("db-transposed", "transposed"),
		DatabasesVariant("db-describe", "describe"),
		DatabasesVariant("db-ddl", "ddl"),
		DatabasesVariant("db-result-edits", "edits"),
		DatabasesVariant("db-submit-card", "submit"),
		DatabasesVariant("db-export-picker", "export"),
		DatabasesVariant("db-import-card", "import"),
		DatabasesVariant("db-plan", "plan"),
		DatabasesVariant("db-plan-card", "plan-card"),
		DatabasesVariant("db-redis-tree", "redis"),
		DatabasesVariant("db-redis-key", "redis-key"),
		DatabasesVariant("db-ssh-host-card", "ssh-host"),
		DatabasesVariant("db-ssh-form", "ssh-form"),
		DatabasesVariant("db-ssl-form", "ssl-form"),
		DatabasesVariant("db-compare-pick", "compare-pick"),
		DatabasesVariant("db-compare", "compare"),
		Containers(),
		ContainersRemoveConfirm(),
		ContainersStopRemoveConfirm(),
		ContainersComposeDownConfirm(),
		ContainersImages(),
		ContainersNetworks(),
		ContainersVolumes(),
		ContainersNetworkUsers(),
		ContainersHostCard("containers-prune", "system-prune"),
		ContainersHostCard("containers-image-prune-confirm", "image-prune"),
		ContainersHostCard("containers-image-remove-confirm", "image-remove"),
		ContainersHostCard("containers-image-in-use", "image-in-use"),
		ContainersHostCard("containers-network-remove-confirm", "network-remove"),
		ContainersHostCard("containers-network-prune-confirm", "network-prune"),
		ContainersHostCard("containers-volume-remove-confirm", "volume-remove"),
		ContainersHostCard("containers-volume-in-use", "volume-in-use"),
		ContainersHostCard("containers-volume-prune-confirm", "volume-prune"),
		ContainersVariant("containers-registries", "registries"),
		ContainersVariant("containers-registry-connect", "registry-connect"),
		ContainersVariant("containers-help", "help"),
		ContainersVariant("containers-filter", "filter"),    // CP-12
		ContainersVariant("containers-remote-tags", "tags"), // CP-12
		HTTPFile(),
		HTTPResponse(),
		SecretsScene("secrets-migrate-card", "card"),
		SecretsScene("secrets-store-locked", "locked"),
		SecretsScene("secrets-store", "open"),
	}, append(dbCardScenes(), ctCardScenes()...)...)
}

// ctCardScenes are the Containers overlays and pages of the CT-15 set
// (app containersDemo): each runs at every golden size and at 80x24.
func ctCardScenes() []Scene {
	return []Scene{
		// The AI asks to stop a container: name and 12-char id.
		ContainersVariant("containers-ai-card", "ai-card"),
		// Follow logs: a terminal tab with canned lines.
		// Inspect: the read-only JSON page in the editor.
		{Name: "containers-inspect", Steps: WithEditor(Act("contdemo:inspect"), Pause(1000*time.Millisecond))},
		// Docker Hub search results (the picker).
		ContainersVariant("containers-search", "search"),
		// ? on a container: the key card (X-1).
		ContainersVariant("containers-keys-card", "keys"),
		// Live CPU and memory: charts under the selected container (G-DST).
		ContainersVariant("containers-stats", "stats"),
		{Name: "containers-logs", Steps: WithEditor(Act("contdemo:logs"), Pause(1500*time.Millisecond))},
	}
}

func scmStepsNoEditor() []Step { return []Step{Act("view:git"), Pause(1500 * time.Millisecond)} }

var goldenSizes = [][2]int{{150, 42}, {100, 30}}

func goldenExtras() []Scene {
	return []Scene{
		{Name: "theme-light", Theme: "catppuccin-latte", Steps: EditorSteps()},
		{Name: "theme-dark", Theme: "tokyo-night", Steps: EditorSteps()},
		{Name: "icons-unicode", Icons: "unicode", Steps: EditorSteps()},
		{Name: "icons-ascii", Icons: "ascii", Steps: EditorSteps()},
		MarkdownPreview(),
		setupCardScene(),
		// The AI step's chooser (↓ past START to the AI row, Enter).
		{Name: "setup-card-ai-chooser", SetupCard: true, Env: setupCardScene().Env,
			Steps: append(setupCardScene().Steps, Key("down"), Key("down"), Key("down"), Key("down"), Key("down"), Key("down"), Key("enter"), Pause(400*time.Millisecond))},
		// The start page: a returning user, the light theme, ASCII icons.
		DashboardRecent(),
		// The cursor moved down onto the second recent file.
		{Name: "dashboard-recent-cursor", Steps: append(DashboardRecent().Steps, Key("down"), Key("down"), Key("down"), Key("down"), Key("down"), Key("down"), Key("down")),
			Recents: DashboardRecent().Recents, Workspaces: DashboardRecent().Workspaces},
		withTheme(withName(DashboardRecent(), "theme-catppuccin-latte-dashboard"), "catppuccin-latte"),
		func() Scene { sc := withName(DashboardRecent(), "dashboard-ascii"); sc.Icons = "ascii"; return sc }(),
		{Name: "md-preview-light", Theme: "catppuccin-latte", Steps: MarkdownPreview().Steps},
		DiffReview(),
		// Persian (right-to-left) chat: shaped letters in reading order.
		{Name: "ai-chat-fa", Env: aiEnv, Steps: WithEditor(Act("palette:ai-open-chat"), Act("aidemo-fa"), Pause(600*time.Millisecond))},
		{Name: "diff-review-light", Theme: "catppuccin-latte", Steps: DiffReview().Steps},
		// Source Control: compare two commits; the commit view in a light
		// theme and with ASCII icons.
		SCMCompare(),
		{Name: "scm-commit-light", Theme: "catppuccin-latte", Steps: SCMCommit().Steps},
		{Name: "scm-commit-ascii", Icons: "ascii", Steps: SCMCommit().Steps},
		// A running AI turn: the activity line under the last message
		// (a tool running) and the stall warning (B1).
		{Name: "ai-chat-activity", Env: aiEnv, Steps: WithEditor(Act("palette:ai-open-chat"), Act("aidemo-busy:tool"), Pause(600*time.Millisecond))},
		{Name: "ai-chat-stall", Env: aiEnv, Steps: WithEditor(Act("palette:ai-open-chat"), Act("aidemo-busy:stall"), Pause(600*time.Millisecond))},
		// The Services view: Docker down, no services, light, ASCII.
		ServicesVariant("services-docker-down", "down"),
		ServicesVariant("services-empty", "empty"),
		{Name: "services-light", Theme: "catppuccin-latte", Steps: Services().Steps},
		{Name: "services-ascii", Icons: "ascii", Steps: Services().Steps},
		{Name: "db-table-ascii", Icons: "ascii", Steps: DatabasesVariant("db-table", "table").Steps},
		{Name: "db-result-edits-ascii", Icons: "ascii", Steps: DatabasesVariant("db-result-edits", "edits").Steps},
		{Name: "db-result-edits-light", Theme: "catppuccin-latte", Steps: DatabasesVariant("db-result-edits", "edits").Steps},
		{Name: "db-plan-ascii", Icons: "ascii", Steps: DatabasesVariant("db-plan", "plan").Steps},
		{Name: "db-plan-card-ascii", Icons: "ascii", Steps: DatabasesVariant("db-plan-card", "plan-card").Steps},
		{Name: "db-result-ascii", Icons: "ascii", Steps: DatabasesVariant("db-result", "result").Steps},
		{Name: "db-result-light", Theme: "catppuccin-latte", Steps: DatabasesVariant("db-result", "result").Steps},
		{Name: "db-import-card-ascii", Icons: "ascii", Steps: DatabasesVariant("db-import-card", "import").Steps},
		{Name: "db-tree-ascii", Icons: "ascii", Steps: Databases().Steps},
		{Name: "db-redis-tree-ascii", Icons: "ascii", Steps: DatabasesVariant("db-redis-tree", "redis").Steps},
		{Name: "db-tree-light", Theme: "catppuccin-latte", Steps: Databases().Steps},
		{Name: "db-stopped-hint-ascii", Icons: "ascii", Steps: DatabasesVariant("db-stopped-hint", "stopped").Steps},
		{Name: "db-compare-ascii", Icons: "ascii", Steps: DatabasesVariant("db-compare", "compare").Steps},
		// Live CPU and memory (G-DST): a wide sidebar whose rows show the
		// values, and the charts in ASCII icons.
		ContainersVariant("containers-stats-wide", "stats-wide"),
		{Name: "containers-stats-ascii", Icons: "ascii", Steps: ContainersVariant("containers-stats", "stats").Steps},
		ContainersVariant("containers-menu", "menu"),
		ContainersVariant("containers-docker-down", "down"),
		ContainersVariant("containers-no-engine", "noengine"),
		{Name: "containers-light", Theme: "catppuccin-latte", Steps: Containers().Steps},
		{Name: "containers-ascii", Icons: "ascii", Steps: Containers().Steps},
		{Name: "containers-menu-light", Theme: "catppuccin-latte", Steps: ContainersVariant("containers-menu", "menu").Steps},
		{Name: "containers-menu-ascii", Icons: "ascii", Steps: ContainersVariant("containers-menu", "menu").Steps},
		// A clean-up card in a light theme and with ASCII icons.
		{Name: "containers-prune-light", Theme: "catppuccin-latte", Steps: ContainersHostCard("containers-prune", "system-prune").Steps},
		{Name: "containers-prune-ascii", Icons: "ascii", Steps: ContainersHostCard("containers-prune", "system-prune").Steps},
		// The HTTP Response page: the Headers view, and in a light theme.
		{Name: "http-response-headers", Steps: []Step{Act("httpdemo:headers"), Pause(900 * time.Millisecond)}},
		withTheme(withName(HTTPResponse(), "http-response-light"), "catppuccin-latte"),
		// The Problems panel with nothing to show (the empty state).
		{Name: "problems", Steps: WithEditor(Act("palette:problems-panel"), Pause(800*time.Millisecond))},
	}
}

// setupCardScene is the first-run setup checklist (a fresh config). kitty
// bundles the Nerd symbols, so the detection is the same on every machine.
func setupCardScene() Scene {
	return Scene{Name: "setup-card", SetupCard: true, Env: map[string]string{"TERM": "xterm-kitty"}, Steps: []Step{Pause(1500 * time.Millisecond)}}
}

func goldenCases() []goldenCase {
	var out []goldenCase
	for _, sc := range goldenScenes() {
		for _, sz := range goldenSizes {
			out = append(out, goldenCase{fmt.Sprintf("%s-%dx%d", sc.Name, sz[0], sz[1]), sc, sz[0], sz[1]})
		}
	}
	for _, sc := range []Scene{ContainersNetworks(), ContainersVolumes(), ContainersNetworkUsers()} {
		out = append(out, goldenCase{fmt.Sprintf("%s-150x55", sc.Name), sc, 150, 55})
	}
	for _, sc := range goldenExtras() {
		sz := goldenSizes[0]
		out = append(out, goldenCase{fmt.Sprintf("%s-%dx%d", sc.Name, sz[0], sz[1]), sc, sz[0], sz[1]})
	}
	// The smallest common window (80x24) for the screens that have a
	// compact layout: start page, editor, palette, toast.
	for _, sc := range []Scene{Dashboard(), DashboardRecent(), setupCardScene(), Editor(), Palette(), Toast(), FindResults(), SCMCommit(), TerminalMarks(), Services(), Databases(), DatabasesVariant("db-empty", "empty"), DatabasesVariant("db-no-sqlite", "no-sqlite"), DatabasesVariant("db-stopped-hint", "stopped"), ContainersVariant("containers-db-service-menu", "db-menu"), ContainersVariant("containers-db-container-menu", "db-container-menu"), DatabasesVariant("db-result", "result"), DatabasesVariant("db-messages", "messages"), DatabasesVariant("db-error", "error"), DatabasesVariant("db-table", "table"), DatabasesVariant("db-transposed", "transposed"), DatabasesVariant("db-describe", "describe"), DatabasesVariant("db-ddl", "ddl"), DatabasesVariant("db-result-edits", "edits"), DatabasesVariant("db-submit-card", "submit"), DatabasesVariant("db-export-picker", "export"), DatabasesVariant("db-import-card", "import"), DatabasesVariant("db-plan", "plan"), DatabasesVariant("db-plan-card", "plan-card"), DatabasesVariant("db-redis-tree", "redis"), DatabasesVariant("db-redis-key", "redis-key"), DatabasesVariant("db-ssh-host-card", "ssh-host"), DatabasesVariant("db-ssh-form", "ssh-form"), DatabasesVariant("db-ssl-form", "ssl-form"), DatabasesVariant("db-compare-pick", "compare-pick"), DatabasesVariant("db-compare", "compare"), Containers(), ContainersRemoveConfirm(), ContainersStopRemoveConfirm(), ContainersComposeDownConfirm(), ContainersImages(), ContainersNetworks(), ContainersVolumes(), ContainersHostCard("containers-prune", "system-prune"), ContainersHostCard("containers-image-prune-confirm", "image-prune"), ContainersHostCard("containers-image-remove-confirm", "image-remove"), ContainersHostCard("containers-image-in-use", "image-in-use"), ContainersHostCard("containers-network-remove-confirm", "network-remove"), ContainersHostCard("containers-network-prune-confirm", "network-prune"), ContainersHostCard("containers-volume-remove-confirm", "volume-remove"), ContainersHostCard("containers-volume-in-use", "volume-in-use"), ContainersHostCard("containers-volume-prune-confirm", "volume-prune"), HTTPFile(), HTTPResponse(), HTTPResponseTLS()} {
		out = append(out, goldenCase{fmt.Sprintf("%s-80x24", sc.Name), sc, 80, 24})
	}
	for _, sc := range append(dbCardScenes(), ctCardScenes()...) {
		out = append(out, goldenCase{fmt.Sprintf("%s-80x24", sc.Name), sc, 80, 24})
	}
	out = append(out, goldenCase{"setup-card-100x30", setupCardScene(), 100, 30})
	// A two-step agent pipeline run, running and done (U20 AP-4).
	for _, v := range []string{"running", "done"} {
		sc := AIPipeline(v)
		out = append(out, goldenCase{sc.Name + "-150x42", sc, 150, 42}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	// Work until done (OM-3): verify passes in round 2, or the run gives
	// up after the last round.
	for _, v := range []string{"done", "fail"} {
		sc := AIUntilDone(v)
		out = append(out, goldenCase{sc.Name + "-150x42", sc, 150, 42}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	// The agent profile chip, its picker and the project profile trust
	// card (U20 AP-5).
	for _, v := range []string{"chip", "picker", "trust", "save"} {
		sc := AIProfile(v)
		out = append(out, goldenCase{sc.Name + "-150x42", sc, 150, 42}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	// The profile step form (AH-5): a copy of a built-in, a copy with a
	// problem shown inline, the Remove step question over it, the model
	// picker (AIP-6: open, searching, nothing found, the Add AI card it
	// opens) and steps whose AI is not ready (Fix), at 200x50 and 80x24.
	for _, v := range []string{"form", "form-error", "form-remove", "form-picker", "form-picker-search", "form-picker-empty", "form-picker-add", "form-needs"} {
		sc := AIProfile(v)
		out = append(out, goldenCase{sc.Name + "-200x50", sc, 200, 50}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	// The AI memory line, the ask-once card, AI: Memory… and the Forget
	// card (OM-2).
	for _, v := range []string{"chat", "ask", "picker", "forget"} {
		sc := AIMemory(v)
		out = append(out, goldenCase{sc.Name + "-150x42", sc, 150, 42}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	// The skill chip, AI: Skills… and the project skills trust card (OM-4).
	for _, v := range []string{"chat", "picker", "trust"} {
		sc := AISkills(v)
		out = append(out, goldenCase{sc.Name + "-150x42", sc, 150, 42}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	// Runs: the list and overview, a run's page, a stalled run and the
	// empty page, a live subscription CLI run and one paused by its usage
	// limit (CS-3/4) at 200x50 and 80x24 (the compact forms); the list in a
	// light theme and in ASCII icons.
	for _, v := range []string{"list", "detail", "stall", "empty", "cli", "cli-limit"} {
		sc := Runs(v)
		out = append(out, goldenCase{sc.Name + "-200x50", sc, 200, 50}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	{
		sc := Runs("list")
		light, ascii := withTheme(withName(sc, sc.Name+"-light"), "catppuccin-latte"), withName(sc, sc.Name+"-ascii")
		ascii.Icons = "ascii"
		out = append(out, goldenCase{light.Name + "-200x50", light, 200, 50}, goldenCase{ascii.Name + "-200x50", ascii, 200, 50},
			goldenCase{sc.Name + "-150x42", sc, 150, 42})
	}
	// Itermoc (AH-4): a working setup, a fresh install and the actions menu at
	// the presentation size and at 80x24; the full view also in a light theme
	// and in ASCII icons.
	// The MODELS cards (AH-6): the Add slot form, the Set key offer and
	// the Remove card.
	// The service page (B3), the outside-agent pages (B4) and the overview
	// (B5); at 80x24 every one is the list / page toggle (B6).
	// The cards of the AI write tools (AH-8): Deny first, the draft as a diff.
	// The Add AI card (AIP-3) at each step of the key, local and URL paths.
	// AI PROVIDERS (AIP-4): an open provider row with its page, and no AI yet.
	for _, v := range []string{"list", "profile", "follow", "empty", "menu", "slotadd", "slotkey", "slotremove", "service", "agent", "agents", "overview",
		"providers-open", "providers-empty",
		"ai-switch", "ai-draft", "ai-slot", "ai-connect", "ai-access", "ai-start",
		"addai-pick", "addai-method", "addai-key", "addai-key-busy", "addai-key-bad", "addai-local", "addai-url", "addai-done"} {
		sc := Itermoc(v)
		out = append(out, goldenCase{sc.Name + "-200x50", sc, 200, 50}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	// The Add AI card's sign-in path (AIP-5): ask, install, browser, the
	// paste-code field, the device code over SSH, no Node.js, ready.
	for _, v := range []string{"ask", "install", "browser", "code", "device", "nonode", "ready"} {
		sc := AddAISignIn(v)
		out = append(out, goldenCase{sc.Name + "-200x50", sc, 200, 50}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	{
		sc := Itermoc("list")
		p := Itermoc("profile")
		pl, pa := withTheme(withName(p, p.Name+"-light"), "catppuccin-latte"), withName(p, p.Name+"-ascii")
		pa.Icons = "ascii"
		out = append(out, goldenCase{pl.Name + "-200x50", pl, 200, 50}, goldenCase{pa.Name + "-200x50", pa, 200, 50})
		light, ascii := withTheme(withName(sc, sc.Name+"-light"), "catppuccin-latte"), withName(sc, sc.Name+"-ascii")
		ascii.Icons = "ascii"
		out = append(out, goldenCase{light.Name + "-200x50", light, 200, 50}, goldenCase{ascii.Name + "-200x50", ascii, 200, 50})
	}
	// AI: Show Profile and AI: Usage (AU-5), at both sizes, and the four
	// screens of the "see your agents and what they cost" story at full
	// size (200x50) for the presentation.
	for _, sc := range []Scene{AIProfile("show"), AIUsage()} {
		out = append(out, goldenCase{sc.Name + "-150x42", sc, 150, 42}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	for _, sc := range []Scene{AIProfile("picker"), AIProfile("show"), AgentsPanel("done"), AIUsage()} {
		out = append(out, goldenCase{sc.Name + "-200x50", sc, 200, 50})
	}
	// The finished run and the trust card in a light theme and in ASCII
	// icons (U20 AP-7).
	for _, sc := range []Scene{AIPipeline("done"), AIProfile("trust")} {
		light, ascii := withTheme(withName(sc, sc.Name+"-light"), "catppuccin-latte"), withName(sc, sc.Name+"-ascii")
		ascii.Icons = "ascii"
		out = append(out, goldenCase{light.Name + "-150x42", light, 150, 42}, goldenCase{ascii.Name + "-150x42", ascii, 150, 42})
	}
	// The AGENTS view of a planned diamond run (U20 AP-9), running and
	// done, at 150x42 and 80x24; the running one also in a light theme
	// and in ASCII icons.
	for _, v := range []string{"running", "done"} {
		sc := AgentsPanel(v)
		out = append(out, goldenCase{sc.Name + "-150x42", sc, 150, 42}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	{
		sc := AgentsPanel("running")
		light, ascii := withTheme(withName(sc, sc.Name+"-light"), "catppuccin-latte"), withName(sc, sc.Name+"-ascii")
		ascii.Icons = "ascii"
		out = append(out, goldenCase{light.Name + "-150x42", light, 150, 42}, goldenCase{ascii.Name + "-150x42", ascii, 150, 42})
	}
	// The hover label of the git branch with its "More info" line, the
	// pointer on that line (U21 CH-3).
	{
		sc := HelpTooltip()
		out = append(out, goldenCase{sc.Name + "-150x42", sc, 150, 42}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	for _, sc := range []Scene{WelcomeHover(), ExplorerHover()} {
		out = append(out, goldenCase{sc.Name + "-150x42", sc, 150, 42}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	// The in-app guide at the Chat panel section, in both window sizes,
	// light theme and ASCII icon mode (U21 CH-5).
	{
		sc := HelpViewer()
		out = append(out, goldenCase{sc.Name + "-150x42", sc, 150, 42}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
		light, ascii := withTheme(withName(sc, sc.Name+"-light"), "catppuccin-latte"), withName(sc, sc.Name+"-ascii")
		ascii.Icons = "ascii"
		out = append(out, goldenCase{light.Name + "-150x42", light, 150, 42}, goldenCase{ascii.Name + "-150x42", ascii, 150, 42})
	}
	// Import from Another Editor (EM-4b): the source picker and the
	// review panel, with sample editors in the sandbox config dir.
	for _, sc := range []Scene{ImportPicker(), ImportPanel(), ImportPickerEmpty(), ImportFilePrompt(), ImportConfirm(), ImportUndo(), ImportSetupRow(), ImportSnippets()} {
		out = append(out, goldenCase{sc.Name + "-150x42", sc, 150, 42}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	// The common middle window (100x30) for the import screens.
	for _, sc := range []Scene{ImportPicker(), ImportPanel(), ImportConfirm(), ImportSetupRow()} {
		out = append(out, goldenCase{sc.Name + "-100x30", sc, 100, 30})
	}
	// The review panel in a light theme and in ASCII icons.
	{
		sc := ImportPanel()
		light, ascii := withTheme(withName(sc, sc.Name+"-light"), "catppuccin-latte"), withName(sc, sc.Name+"-ascii")
		ascii.Icons = "ascii"
		out = append(out, goldenCase{light.Name + "-150x42", light, 150, 42}, goldenCase{ascii.Name + "-150x42", ascii, 150, 42})
	}
	// Connect a service (CX-5): every wizard step at 160x40 and 80x24;
	// the pick and done steps also in a light theme and in ASCII icons.
	for _, sc := range connectScenes() {
		out = append(out, goldenCase{sc.Name + "-160x40", sc, 160, 40}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	for _, step := range []string{"pick", "done"} {
		sc := connectScene(step)
		light, ascii := withTheme(withName(sc, sc.Name+"-light"), "catppuccin-latte"), withName(sc, sc.Name+"-ascii")
		ascii.Icons = "ascii"
		out = append(out, goldenCase{light.Name + "-160x40", light, 160, 40}, goldenCase{ascii.Name + "-160x40", ascii, 160, 40})
	}
	// termocode as an MCP server (M1 MS-3): the status screen and an
	// outside agent's card, at both sizes, in a light theme and in ASCII
	// icons.
	for _, v := range []string{"status", "card"} {
		sc := MCPServeScene("mcp-serve-"+v, v)
		light, ascii := withTheme(withName(sc, sc.Name+"-light"), "catppuccin-latte"), withName(sc, sc.Name+"-ascii")
		ascii.Icons = "ascii"
		out = append(out, goldenCase{sc.Name + "-150x42", sc, 150, 42}, goldenCase{sc.Name + "-80x24", sc, 80, 24},
			goldenCase{light.Name + "-150x42", light, 150, 42}, goldenCase{ascii.Name + "-150x42", ascii, 150, 42})
	}
	// The MCP server's other screens (MCP-13): the quiet status bar chip,
	// OpenCode's "add termocode" card, the hand tip of Gemini CLI and Aider, the Agent Auto-Approve list, the status screen while nothing listens.
	for _, sc := range []Scene{MCPServeScene("mcp-serve-chip", "chip"), MCPServeScene("mcp-serve-opencode-card", "opencode"),
		MCPServeScene("mcp-serve-tip-gemini", "tip-gemini"), MCPServeScene("mcp-serve-tip-aider", "tip-aider"),
		MCPServeScene("mcp-serve-allow-picker", "allow"),
		MCPServeScene("mcp-serve-idle", "idle"), MCPServeScene("mcp-serve-off", "off")} {
		out = append(out, goldenCase{sc.Name + "-150x42", sc, 150, 42}, goldenCase{sc.Name + "-80x24", sc, 80, 24})
	}
	return out
}

// connectScene is one step of the Connect wizard with sample data.
func connectScene(step string) Scene {
	return Scene{Name: "mcp-connect-" + step, Steps: []Step{Pause(800 * time.Millisecond), Act("connectdemo:" + step), Pause(600 * time.Millisecond)}}
}

func connectScenes() []Scene {
	var out []Scene
	for _, step := range []string{"pick", "gh", "device", "browser", "ssh", "done", "error", "token", "slack", "paste"} {
		out = append(out, connectScene(step))
	}
	return out
}

func TestMain(m *testing.M) {
	if name := os.Getenv(childCaseEnv); name != "" {
		os.Exit(runChild(name))
	}
	os.Exit(m.Run())
}

// runChild renders one case inside the hermetic child process.
func runChild(name string) int {
	clock.Set(func() time.Time { return DemoNow })
	for _, c := range goldenCases() {
		if c.name != name {
			continue
		}
		frame, err := Run(c.scene, c.w, c.h, Options{Settle: true, Quiet: true, FinalQuiet: 2 * time.Second,
			// A fixed name: the secret store screen shows its path.
			ConfigDir: filepath.Join(os.TempDir(), "shots-cfg")})
		if err != nil {
			fmt.Fprintln(os.Stderr, "render:", err)
			return 1
		}
		if err := os.WriteFile(os.Getenv(childOutEnv), []byte(frame), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "unknown golden case", name)
	return 2
}

func TestGoldenScreens(t *testing.T) {
	if testing.Short() {
		t.Skip("golden screens start nvim; skipped with -short")
	}
	if runtime.GOOS == "windows" {
		t.Skip("golden screens need a Unix shell")
	}
	bin := hermeticBin(t)
	updating := *update || os.Getenv("UPDATE_GOLDEN") == "1"
	if updating {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sem := make(chan struct{}, goldenJobs())
	for _, c := range goldenCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			sem <- struct{}{}
			defer func() { <-sem }()
			got := renderInChild(t, bin, c)
			if !utf8.ValidString(got) {
				t.Errorf("%s: frame is not valid UTF-8 (a multi-byte glyph was cut)", c.name)
			}
			golden := filepath.Join(goldenDir, c.name+".ans")
			actual := filepath.Join(goldenDir, c.name+"_actual.ans")
			if updating {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				_ = os.Remove(actual)
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("no golden frame %s (run with -update to create it): %v", golden, err)
			}
			if string(want) == got {
				_ = os.Remove(actual)
				return
			}
			_ = os.WriteFile(actual, []byte(got), 0o644)
			msg := frameDiff(string(want), got)
			if pngs := writeReviewPNGs(c, string(want), got); pngs != "" {
				msg += "\n" + pngs
			}
			t.Errorf("%s changed.\n%s\nactual frame: %s\nIf the change is intended, accept it with:\n  go test ./internal/devtools/shotkit -run 'TestGoldenScreens/%s' -update",
				c.name, msg, actual, c.name)
		})
	}
}

// goldenJobs is how many child apps run at once (SHOTKIT_GOLDEN_JOBS).
func goldenJobs() int {
	if n, err := strconv.Atoi(os.Getenv("SHOTKIT_GOLDEN_JOBS")); err == nil && n > 0 {
		return n
	}
	n := runtime.NumCPU() / 2
	if n < 2 {
		n = 2
	}
	if n > 8 {
		n = 8
	}
	return n
}

// hermeticBin makes a PATH directory with only the tools the scenes need.
// It skips the test when nvim or git is missing.
func hermeticBin(t *testing.T) string {
	bin := t.TempDir()
	for _, tool := range []string{"nvim", "git", "sh", "ls"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			if tool == "nvim" || tool == "git" {
				t.Skipf("golden screens need %s on PATH", tool)
			}
			continue
		}
		if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

// renderInChild builds a private home + demo workspace for the case and
// renders it in a child process with a fixed environment.
func renderInChild(t *testing.T, bin string, c goldenCase) string {
	// A fixed-length path under /tmp (not $TMPDIR, which differs between
	// machines), so a screen that shows a path has the same layout
	// everywhere; the random part is masked in the frame below.
	tag := fmt.Sprintf("%08x", rand.Uint32())
	root := "/tmp/termocode-golden-" + tag
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	home := filepath.Join(root, "home")
	plugins := filepath.Join(home, ".local", "share", "termocode", "plugins")
	// Empty stand-ins for the auto-cloned nvim plugins, so nothing is
	// fetched from the network and frames do not depend on what a machine
	// has installed.
	for _, d := range []string{
		filepath.Join(plugins, "vim-visual-multi", ".git"),
		filepath.Join(plugins, "nvim-dap", ".git"),
		filepath.Join(plugins, "rainbow-delimiters.nvim", "lua", "rainbow-delimiters"),
		filepath.Join(root, "tmp"),
		filepath.Join(root, "tools"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(plugins, "rainbow-delimiters.nvim", "lua", "rainbow-delimiters", "setup.lua"), []byte("return {}\n"), 0o644)
	// No "No language server for Go" toast over every editor screen.
	_ = os.WriteFile(filepath.Join(root, "tools", "asked.json"),
		[]byte(`{"filetypes": ["go", "markdown", "lua", "sh", "json", "gomod"]}`), 0o644)
	src, err := filepath.Abs("testdata/workspace")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := DemoWorkspace(src, home)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "frame.ans")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// A stuck child (e.g. a hung nvim) must not hang the test: a hang
	// ends in a kill, and the test's own cleanup then removes root.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = ws
	cmd.Env = []string{
		childCaseEnv + "=" + c.name,
		childOutEnv + "=" + out,
		"PATH=" + bin,
		"HOME=" + home,
		"PWD=" + ws,
		"USER=demo", "LOGNAME=demo",
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"TMPDIR=" + filepath.Join(root, "tmp"),
		"TERM=xterm-256color", "COLORTERM=truecolor",
		"LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC",
		"SHELL=/bin/sh", "PS1=$ ",
		"GIT_CONFIG_NOSYSTEM=1",
		"TERMOCODE_PLUGINS_DIR=" + plugins,
		"TERMOCODE_TOOLS_DIR=" + filepath.Join(root, "tools"),
		"TERMOCODE_AI_BASE_URL=http://127.0.0.1:9",
	}
	if v := os.Getenv("GOCOVERDIR"); v != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+v)
	}
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("render %s: %v\n%s", c.name, err, b)
	}
	frame, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(frame), tag, "XXXXXXXX")
}

// frameDiff describes how got differs from want: the changed rows as plain
// text, or a note when only colours / styles changed.
func frameDiff(want, got string) string {
	wRows, gRows := strings.Split(want, "\n"), strings.Split(got, "\n")
	n := len(wRows)
	if len(gRows) > n {
		n = len(gRows)
	}
	row := func(rows []string, i int) string {
		if i < len(rows) {
			return rows[i]
		}
		return ""
	}
	var b strings.Builder
	textChanged, styleOnly, shown := 0, 0, 0
	for i := 0; i < n; i++ {
		w, g := row(wRows, i), row(gRows, i)
		if w == g {
			continue
		}
		pw, pg := strings.TrimRight(StripANSI(w), " "), strings.TrimRight(StripANSI(g), " ")
		if pw == pg {
			styleOnly++
			continue
		}
		textChanged++
		if shown < 30 {
			shown++
			fmt.Fprintf(&b, "row %2d - %s\n       + %s\n", i, pw, pg)
		}
	}
	if textChanged == 0 {
		return fmt.Sprintf("Only colours / styles changed (%d rows; the text is the same).", styleOnly)
	}
	head := fmt.Sprintf("%d rows changed text", textChanged)
	if styleOnly > 0 {
		head += fmt.Sprintf(", %d more rows changed only colours / styles", styleOnly)
	}
	if shown < textChanged {
		head += fmt.Sprintf(" (first %d shown)", shown)
	}
	return head + ":\n" + b.String()
}

// writeReviewPNGs renders the golden and the actual frame to PNG next to
// the .ans files when TERMOCODE_SHOTS_FONT names a font file.
func writeReviewPNGs(c goldenCase, want, got string) string {
	font := os.Getenv("TERMOCODE_SHOTS_FONT")
	if font == "" {
		return ""
	}
	r, err := ansipng.New(ansipng.Options{
		FontRegular: font, FontBold: strings.Replace(font, "Regular", "Bold", 1), Size: 15,
		DefaultBG: rgba(theme.Hex(theme.BgEditor)), DefaultFG: rgba(theme.Hex(theme.TextPrimary)),
	})
	if err != nil {
		return "png: " + err.Error()
	}
	base := filepath.Join(goldenDir, c.name)
	if err := r.WritePNG(base+"_golden.png", want, c.w); err != nil {
		return "png: " + err.Error()
	}
	if err := r.WritePNG(base+"_actual.png", got, c.w); err != nil {
		return "png: " + err.Error()
	}
	return "review images: " + base + "_golden.png, " + base + "_actual.png"
}

func rgba(h string) color.RGBA {
	var r, g, b uint8
	if _, err := fmt.Sscanf(strings.TrimPrefix(h, "#"), "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.RGBA{0x1e, 0x1e, 0x1e, 0xff}
	}
	return color.RGBA{r, g, b, 0xff}
}
