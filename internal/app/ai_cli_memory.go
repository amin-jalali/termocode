package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
)

// A chat whose profile step runs a coding CLI (Codex, Claude Code)
// starts the CLI again on every turn. So that the next turn knows the
// earlier ones, the step goes on in the CLI's own session (the id the
// last turn's run gave, kept on the step card in the chat file). With no
// session, a resume that fails, or a step that now runs another CLI,
// model or profile, the recent turns go before the new message instead
// (aiCLITranscript).

// aiCLITranscriptBytes caps the recent turns written into a CLI step's
// prompt; aiCLITranscriptMsgBytes caps one message of them.
const (
	aiCLITranscriptBytes    = 16 << 10
	aiCLITranscriptMsgBytes = 4 << 10
)

// aiCLIMemory is what a run's CLI steps know of the chat before this
// turn: the sessions of the last turn's steps and the recent turns as
// text. It is read-only once built (the steps run on their own
// goroutines).
type aiCLIMemory struct {
	off        bool              // ai_cli_resume off: never resume
	profile    string            // the run's profile name and hash
	sessions   map[string]string // step id → session id of the last turn
	keys       map[string]string // step id → the key that session ran with
	transcript string            // the recent turns ("": none)
}

// aiCLIMemoryOf builds the memory of the run at msgIdx (its user message
// is msgIdx-1) from the chat before it; hist is the run's history (the
// system message first, the new user message last).
func (s *aiState) aiCLIMemoryOf(run agentdag.Run, prof ai.Profile, msgIdx int, hist []ai.Message) *aiCLIMemory {
	mem := &aiCLIMemory{off: !s.settings.CLIResumeOn(), profile: run.Profile + " " + ai.ProfileHash(prof),
		sessions: map[string]string{}, keys: map[string]string{}}
	if len(hist) > 2 {
		mem.transcript = aiCLITranscript(hist[1:len(hist)-1], aiCLITranscriptBytes)
	}
	// The last answer before this turn: its steps' sessions hold every
	// turn up to now. A plain chat answer in between: the sessions miss
	// it, so none is resumed.
	for i := msgIdx - 2; i >= 0; i-- {
		sm := s.conv.Messages[i]
		if sm.Role != ai.StoredAssistant {
			continue
		}
		if sm.Run != nil {
			for _, st := range sm.Steps {
				if st.CLISession != "" {
					mem.sessions[st.ID], mem.keys[st.ID] = st.CLISession, st.CLISessionKey
				}
			}
		}
		break
	}
	return mem
}

// key is what node n's session runs with: a new CLI, model or profile
// starts a new session.
func (mem *aiCLIMemory) key(n agentdag.Node) string {
	sum := sha256.Sum256([]byte(n.CLI + "\x00" + n.Model + "\x00" + mem.profile))
	return hex.EncodeToString(sum[:8])
}

// run runs CLI node n: in the last turn's session when it can, else (or
// when that resume fails) in a new one with the recent turns before the
// input. prompt builds the stdin text from what goes before the input.
// The session the run gave is kept on the step's card.
func (mem *aiCLIMemory) run(ctx context.Context, n agentdag.Node, spec clistep.Spec, prompt func(earlier string) string,
	emit func(ai.Event), call aiIDECaller) (clistep.Result, error) {
	key := mem.key(n)
	if id := mem.sessions[n.ID]; !mem.off && id != "" && mem.keys[n.ID] == key {
		spec.Resume = id
		res, err := aiCLIRun(ctx, spec, prompt(""), emit)
		var le *ai.LimitError
		if err == nil || ctx.Err() != nil || errors.As(err, &le) {
			if err == nil && res.Session == "" {
				res.Session = id
			}
			mem.keep(ctx, n.ID, res, err, key, call)
			return res, err
		}
		recordError("[ai] step " + n.ID + ": the CLI session did not resume, a new one starts: " + err.Error())
		mem.note(ctx, n.ID, "the CLI's earlier session could not go on: a new one got the recent turns", call)
		spec.Resume = ""
	}
	earlier := ""
	if mem.transcript != "" {
		earlier = mem.transcript + "\n\n### The new message\n"
	}
	res, err := aiCLIRun(ctx, spec, prompt(earlier), emit)
	mem.keep(ctx, n.ID, res, err, key, call)
	return res, err
}

// keep writes a finished run's session on step id's card (when resume
// is on and the run gave one).
func (mem *aiCLIMemory) keep(ctx context.Context, id string, res clistep.Result, err error, key string, call aiIDECaller) {
	if mem.off || err != nil || res.Session == "" || call == nil {
		return
	}
	_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
		if st := aiFindStep(m.aiS().aiRunMessage(), id); st != nil {
			st.CLISession, st.CLISessionKey = res.Session, key
		}
		return "", nil, nil
	})
}

// note adds a short line to step id's card.
func (mem *aiCLIMemory) note(ctx context.Context, id, note string, call aiIDECaller) {
	if call == nil {
		return
	}
	_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
		m.aiSetStepWorktree(id, ai.StoredWorktree{}, note)
		return "", nil, nil
	})
}

// aiCLITranscript is the recent turns of msgs as text, oldest first: the
// newest are kept within limit bytes, each message cut at
// aiCLITranscriptMsgBytes, secrets redacted. "" when there are none.
func aiCLITranscript(msgs []ai.Message, limit int) string {
	var parts []string
	size := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		who := ""
		switch msgs[i].Role {
		case ai.RoleUser:
			who = "User: "
		case ai.RoleAssistant:
			who = "You: "
		default:
			continue
		}
		c := strings.TrimSpace(msgs[i].Content)
		if c == "" {
			continue
		}
		c = who + aiCutBytes(c, aiCLITranscriptMsgBytes)
		if size+len(c) > limit {
			break
		}
		size += len(c) + 2
		parts = append(parts, c)
	}
	if len(parts) == 0 {
		return ""
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return "### Earlier in this chat (the recent turns, oldest first)\n" + ai.Redact(strings.Join(parts, "\n\n"))
}

// aiCutBytes is s cut to at most n bytes on a rune boundary, with "…"
// when cut.
func aiCutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n - len("…")
	for cut > 0 && cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
