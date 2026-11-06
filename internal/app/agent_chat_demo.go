package app

import (
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/picker"
)

// agentChatDemoLog is a made-up Claude Code session (no file is read).
const agentChatDemoLog = `{"type":"user","sessionId":"3f9a2c1e-demo","cwd":"/work/demo","gitBranch":"feat/pin-row","timestamp":"2026-11-05T10:00:00Z","message":{"role":"user","content":"Add the pinned row as in the issue. Run go test."}}
{"type":"assistant","sessionId":"3f9a2c1e-demo","cwd":"/work/demo","timestamp":"2026-11-05T10:00:05Z","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"I will add a PINNED group to the worktree list, above the other rows, and keep the order the user chose."},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/work/demo/internal/app/worktrees_view.go"}}],"usage":{"input_tokens":1200,"cache_read_input_tokens":15000,"output_tokens":300}}}
{"type":"user","sessionId":"3f9a2c1e-demo","timestamp":"2026-11-05T10:00:06Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"package app"}]}}
{"type":"assistant","sessionId":"3f9a2c1e-demo","timestamp":"2026-11-05T10:00:20Z","message":{"id":"m2","role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Edit","input":{"file_path":"/work/demo/internal/app/worktrees_view.go","old_string":"a\nb\n","new_string":"a\nb\nc\nd\ne\n"}}],"usage":{"input_tokens":400,"cache_read_input_tokens":16000,"output_tokens":900}}}
{"type":"user","sessionId":"3f9a2c1e-demo","timestamp":"2026-11-05T10:00:21Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"The file has been updated."}]}}
{"type":"assistant","sessionId":"3f9a2c1e-demo","timestamp":"2026-11-05T10:00:30Z","message":{"id":"m3","role":"assistant","content":[{"type":"tool_use","id":"t3","name":"Bash","input":{"command":"go test ./internal/app/ -run Worktree"}}],"usage":{"input_tokens":300,"cache_read_input_tokens":17000,"output_tokens":80}}}
{"type":"user","sessionId":"3f9a2c1e-demo","timestamp":"2026-11-05T10:00:40Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t3","content":"ok"}]}}
{"type":"assistant","sessionId":"3f9a2c1e-demo","timestamp":"2026-11-05T10:00:45Z","message":{"id":"m4","role":"assistant","content":[{"type":"text","text":"Done. The pinned rows sit first; 2 tests added."}],"usage":{"input_tokens":900,"cache_read_input_tokens":18000,"output_tokens":700}}}
{"type":"user","sessionId":"3f9a2c1e-demo","timestamp":"2026-11-05T10:01:00Z","message":{"role":"user","content":"Now commit it."}}
{"type":"assistant","sessionId":"3f9a2c1e-demo","timestamp":"2026-11-05T10:01:04Z","message":{"id":"m5","role":"assistant","content":[{"type":"tool_use","id":"t4","name":"Bash","input":{"command":"git commit -am \"feat: pinned rows\""}}],"usage":{"input_tokens":200,"cache_read_input_tokens":19000,"output_tokens":60}}}
`

// agentChatDemo is Agent: Chat View over agentChatDemoLog (DevActionMsg
// "agentchatdemo:working|waiting", for the goldens).
func (m *Model) agentChatDemo(arg string) {
	s, _ := agentlog.Parse(agentlog.Claude, strings.NewReader(agentChatDemoLog))
	st := agents.Working
	if arg == "waiting" {
		st = agents.Waiting
	}
	agentChatGen++
	m.agentChat = &agentChatState{gen: agentChatGen, agent: "claude", cli: agentlog.Claude, title: "Claude Code",
		loaded: true, sess: s, demo: true, demoState: st}
	m.showAgentChatPanel()
}

// agentSessionsDemo is Agent: Sessions… with made-up rows (DevActionMsg
// "agentsessionsdemo"; no file is read, no CLI runs).
func (m *Model) agentSessionsDemo() {
	day := time.Date(2026, 11, 5, 10, 0, 0, 0, time.Local)
	root := agentSessionsRoot()
	mk := func(cli agentlog.CLI, h int, prompt string, n, tok int, cwd string) agentlog.Summary {
		return agentlog.Summary{CLI: cli, ID: "demo", Path: "/demo/" + prompt, Start: day.Add(-time.Duration(h) * time.Hour),
			FirstPrompt: prompt, Prompts: n, Tokens: tok, Cwd: cwd}
	}
	m.agentSessions = []agentlog.Summary{
		mk(agentlog.Claude, 0, "Add the pinned row as in the issue. Run go test.", 14, 102640, root),
		mk(agentlog.Codex, 2, "Fix the failing test in queue.go", 3, 20000, root+".worktrees/queue-fix"),
		mk(agentlog.Claude, 26, "Why is the explorer slow with 5000 files?", 7, 48200, root),
		{CLI: agentlog.Codex, Path: "/demo/rollout-new.jsonl", Err: "cannot read this session: the file format is not known to this version"},
		mk(agentlog.Codex, 50, "Write the release notes for 0.9", 2, 9100, root),
	}
	m.agentSessionsResume = map[agentlog.CLI]bool{agentlog.Claude: true, agentlog.Codex: true}
	m.picker = picker.NewItems(" Agent sessions ", m.agentSessionItems())
	m.picker.SetNote([]string{agentSessionsNote})
	m.picker.SetWidth(agentSessionsWidth)
	m.picker.SetRowAction("→", "read")
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAgentSessions
}
