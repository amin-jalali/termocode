package gifkit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/devtools/shotkit"
)

const ms = time.Millisecond

// hold sets how long a recording shows the frame after the step.
func hold(st shotkit.Step, d time.Duration) shotkit.Step { st.Hold = d; return st }

// typed sends s key by key; each key's frame shows for per.
func typed(s string, per time.Duration) []shotkit.Step {
	var out []shotkit.Step
	for _, st := range shotkit.TypeKeys(s) {
		out = append(out, hold(st, per))
	}
	return out
}

// live is a step whose frame is taken after wait, settled or not; the
// GIF shows it for as long as the app took (hold).
func live(st shotkit.Step, wait time.Duration) shotkit.Step {
	st.Live, st.Wait, st.Hold = true, wait, wait
	return st
}

// clickText is a left click (the press, which the app acts on) on the
// first cell showing text.
func clickText(text string, after time.Duration) []shotkit.Step {
	at := func(action tea.MouseAction) shotkit.Step {
		return shotkit.Step{Wait: 200 * ms, Build: func(frame string) (tea.Msg, error) {
			for y, line := range strings.Split(shotkit.StripANSI(frame), "\n") {
				if i := strings.Index(line, text); i >= 0 {
					return tea.MouseMsg{X: len([]rune(line[:i])), Y: y, Type: tea.MouseLeft, Action: action, Button: tea.MouseButtonLeft}, nil
				}
			}
			return nil, fmt.Errorf("click: %q not on screen", text)
		}}
	}
	return []shotkit.Step{hold(at(tea.MouseActionPress), after)}
}

// until makes the step wait for text to be on screen.
func until(st shotkit.Step, text string) shotkit.Step { st.Until = text; return st }

// join flattens step groups.
func join(groups ...[]shotkit.Step) []shotkit.Step {
	var out []shotkit.Step
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// repeat is n copies of st.
func repeat(st shotkit.Step, n int) []shotkit.Step {
	out := make([]shotkit.Step, n)
	for i := range out {
		out[i] = st
	}
	return out
}

func one(st shotkit.Step) []shotkit.Step { return []shotkit.Step{st} }

// demoModelEnv points the AI at the demo model on loopback.
func demoModelEnv(url string) map[string]string {
	return map[string]string{"TERMOCODE_AI_PROVIDER": "ollama", "TERMOCODE_AI_MODEL": "demo", "TERMOCODE_AI_BASE_URL": url}
}

// SceneClips are the GIFs played by shotkit scenes. repo is the repository
// root (the fake docker takes its image list from the gifkit sample).
func SceneClips(repo string) []SceneClip {
	act, key, pause := shotkit.Act, shotkit.Key, shotkit.Pause
	// A setup step is run but not recorded.
	setup := func(st shotkit.Step) shotkit.Step { return hold(st, -1) }
	openFile := setup(act("open:internal/statusbar/model.go"))

	// Databases: type a query, see the result, change a row, review the SQL.
	// The tree and the result are the canned local-pg state (no server).
	query := "SELECT id, email, plan, created_at FROM users ORDER BY id DESC;"
	db := join(
		one(openFile),
		one(hold(act("dbdemo"), 900*ms)),
		one(hold(act("dbdemo:console"), 500*ms)),
		typed(query, 22*ms),
		one(hold(act("dbdemo:result"), 1700*ms)),
		one(hold(act("dbdemo:edits"), 1500*ms)),
		one(hold(act("dbdemo:submit"), 2700*ms)),
	)

	// Containers: the real view against a fake docker on PATH.
	containers := join(
		one(openFile),
		one(hold(until(act("palette:containers-focus"), "nginx"), 1000*ms)),
		repeat(hold(key("down"), 300*ms), 3),
		one(hold(key("down"), 600*ms)),
		one(hold(until(key("enter"), "restart requested"), 1600*ms)), // follow the logs of shop-api-1
		one(hold(act("palette:containers-focus"), 700*ms)),
		one(hold(key("r"), 400*ms)), // restart it
		one(hold(pause(1800*ms), 2300*ms)),
	)

	// AI: the demo model proposes a change to a file; the review card
	// shows it; Accept applies it.
	before := "package toast\n\nimport \"time\"\n\n// DefaultDuration is how long a toast stays on screen.\nconst DefaultDuration = 3 * time.Second\n"
	after := "package toast\n\nimport \"time\"\n\n// DefaultDuration is how long a toast stays on screen.\n// Errors stay longer, so that nobody misses them.\nconst (\n\tDefaultDuration = 5 * time.Second\n\tErrorDuration   = 8 * time.Second\n)\n"
	var model *FakeModel
	aiEnv := map[string]string{}
	aiConfirm := join(
		one(setup(act("open:internal/toast/defaults.go"))),
		one(hold(act("palette:ai-open-chat"), 700*ms)),
		typed("Make toasts stay on screen longer", 30*ms),
		one(live(key("enter"), 500*ms)),
		repeat(live(pause(0), 600*ms), 3),
		one(hold(until(pause(900*ms), "Review AI changes"), 1800*ms)), // the review card
		one(live(key("a"), 400*ms)),                                   // Accept
		repeat(live(pause(0), 400*ms), 2),
		one(hold(until(pause(1200*ms), "Done. Toasts now"), 2200*ms)),
	)

	// Agents: a planned run on demo models, building up live; then u.
	agents := join(
		one(openFile),
		one(setup(act("palette:ai-open-chat"))),
		one(live(act("agentsdemo:live"), 400*ms)),
		repeat(live(pause(0), 500*ms), 13),
		one(hold(pause(600*ms), 1500*ms)),
		one(hold(until(key("u"), "THIS SESSION"), 3200*ms)),
	)

	// MCP: an outside agent asks, the card names it, Allow, the chip.
	mcp := join(
		one(openFile),
		one(hold(act("mcpservedemo:chip"), 1400*ms)),
		one(hold(until(act("mcpservedemo:card"), "wants to run task"), 2200*ms)),
		one(hold(key("left"), 450*ms)),
		one(hold(key("left"), 800*ms)), // Deny -> Always allow -> Allow
		one(hold(key("enter"), 1300*ms)),
		one(hold(until(act("mcpservedemo:status"), "LAST CALLS"), 2600*ms)),
	)

	// Secrets: names only; lock; unlock with the masked passphrase.
	secrets := join(
		one(openFile),
		one(hold(act("secretsdemo:open"), 1900*ms)),
		repeat(hold(key("down"), 250*ms), 4),
		one(hold(until(key("enter"), "State: locked"), 1600*ms)), // Lock now
		repeat(hold(key("down"), 220*ms), 4),
		one(hold(until(key("enter"), "Secrets passphrase"), 900*ms)), // Unlock...
		typed("demo-passphrase-123", 35*ms),
		one(hold(until(key("enter"), "State: unlocked"), 2300*ms)),
	)

	// Import: pick an editor, review, apply, the toast, its undo card.
	imp := join(
		one(openFile),
		one(hold(until(act("importdemo:picker"), "Pick an editor"), 1500*ms)),
		one(hold(until(key("enter"), "Will change"), 2300*ms)),
		one(hold(until(key("enter"), "Import 8 changes"), 1200*ms)),
		one(hold(until(key("enter"), "Click to undo"), 1000*ms)), // apply: the toast
		clickText("Click to undo", 1700*ms),
		one(hold(key("esc"), 1300*ms)),
	)

	return []SceneClip{
		{Name: "db", Caption: "Your database, in your terminal.", Scene: shotkit.Scene{Name: "db", Steps: db}},
		{
			Name: "containers", Caption: "Containers without leaving the editor.",
			Prepare: func(tmp string) (func(), error) { return FakeDocker(repo, tmp) },
			Scene:   shotkit.Scene{Name: "containers", Steps: containers},
		},
		{
			Name: "ai-confirm", Caption: "AI asks before it acts.", Note: "demo models",
			Prepare: func(tmp string) (func(), error) {
				if err := os.WriteFile(filepath.Join("internal", "toast", "defaults.go"), []byte(before), 0o644); err != nil {
					return nil, err
				}
				model = NewFakeModel(
					Reply{Text: "I will make toasts stay on screen for five seconds, and give errors eight seconds.", Pace: 110 * ms,
						Tool: "write_file", Args: map[string]any{"path": "internal/toast/defaults.go", "content": after}},
					Reply{Text: "Done. Toasts now stay for 5 s and errors for 8 s.", Pace: 40 * ms},
				)
				for k, v := range demoModelEnv(model.BaseURL()) {
					aiEnv[k] = v
				}
				return func() { model.Close() }, nil
			},
			Scene: shotkit.Scene{Name: "ai-confirm", Env: aiEnv, Steps: aiConfirm},
		},
		{
			Name: "agents", Caption: "Agents plan the work, run in parallel, and show what each one cost.", Note: "demo models",
			Scene: shotkit.Scene{Name: "agents", Env: map[string]string{"TERMOCODE_AI_PROVIDER": "ollama", "TERMOCODE_AI_MODEL": "demo"}, Steps: agents},
		},
		{Name: "mcp", Caption: "Your agents get the IDE's tools.", Scene: shotkit.Scene{Name: "mcp", Steps: mcp}},
		{
			Name: "secrets", Caption: "Secrets stay secret.", ConfigDir: "/tmp/shots-cfg", // shown on the screen, so a fixed path
			// One sample entry is ${cmd:gh auth token}: a stub, never the real gh.
			Prepare: func(tmp string) (func(), error) { return FakeCommand(tmp, "gh", "echo demo-token-not-real") },
			Scene:   shotkit.Scene{Name: "secrets", Steps: secrets},
		},
		{Name: "import", Caption: "Bring your setup with you.", Scene: shotkit.Scene{Name: "import", Steps: imp}},
	}
}
