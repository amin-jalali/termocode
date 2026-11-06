package shotkit

import (
	"fmt"
	"time"
)

// agentPickerScene is Agent: Start… (AGX-1) with made-up programs on PATH
// and sign-in states (app agentPickerDemo; no CLI runs).
func agentPickerScene(name string) Scene {
	return Scene{Name: name, Steps: WithEditor(Act("agentpickerdemo"), Pause(600*time.Millisecond))}
}

// agentPickerCases are the agent picker at 150x42 and 80x24, and with
// ASCII icons.
func agentPickerCases() []goldenCase {
	var out []goldenCase
	sc := agentPickerScene("agent-picker")
	for _, sz := range [][2]int{{150, 42}, {80, 24}} {
		out = append(out, goldenCase{fmt.Sprintf("%s-%dx%d", sc.Name, sz[0], sz[1]), sc, sz[0], sz[1]})
	}
	ascii := agentPickerScene("agent-picker-ascii")
	ascii.Icons = "ascii"
	return append(out, goldenCase{"agent-picker-ascii-150x42", ascii, 150, 42})
}

// agentChatScene is Agent: Chat View (AGX-8) over a made-up Claude Code
// session (app agentChatDemo; no file is read): arg working or waiting.
func agentChatScene(name, arg string) Scene {
	return Scene{Name: name, Steps: WithEditor(Act("agentchatdemo:"+arg), Pause(600*time.Millisecond))}
}

// agentChatCases are the chat view working and waiting at 150x42, and
// narrow at 80x24.
func agentChatCases() []goldenCase {
	return []goldenCase{
		{"agent-chat-working-150x42", agentChatScene("agent-chat-working", "working"), 150, 42},
		{"agent-chat-waiting-150x42", agentChatScene("agent-chat-waiting", "waiting"), 150, 42},
		{"agent-chat-narrow-80x24", agentChatScene("agent-chat-narrow", "working"), 80, 24},
	}
}

// agentSessionsCases are Agent: Sessions… (AGX-9) with made-up sessions
// (app agentSessionsDemo; no file is read) at 150x42 and 80x24.
func agentSessionsCases() []goldenCase {
	sc := Scene{Name: "agent-sessions", Steps: WithEditor(Act("agentsessionsdemo"), Pause(600*time.Millisecond))}
	return []goldenCase{{"agent-sessions-150x42", sc, 150, 42}, {"agent-sessions-80x24", sc, 80, 24}}
}
