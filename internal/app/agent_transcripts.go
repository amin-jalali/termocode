package app

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/agentlog"
)

// ── Reading the agents' own session files (AGX-7, owner decision D1) ────
//
// Claude Code and Codex write each session to a JSONL file in their own
// folders. With agent_transcripts = read (off by default) termocode reads
// them, as data and read only, for the chat view, the session history
// and the search; internal/agentlog redacts every text first. While the
// setting is off nothing under those folders is opened: every entry point
// goes through agentTranscriptsOn.

// errAgentTranscriptsOff is a read asked for while the setting is off.
var errAgentTranscriptsOff = errors.New("reading agent session files is off (agent_transcripts)")

// agentTranscriptsOn reads config.json agent_transcripts.
func agentTranscriptsOn() bool {
	return strings.EqualFold(strings.TrimSpace(loadSettings().AgentTranscripts), "read")
}

// agentlogHomes are the CLIs' folders of this user (HOME, or their own
// env variables).
func agentlogHomes() agentlog.Homes {
	home, _ := os.UserHomeDir()
	return agentlog.HomesFrom(home, os.Getenv)
}

// agentTranscriptFind is the newest session file of cli for the folder
// cwd changed since since; errAgentTranscriptsOff while off.
func agentTranscriptFind(cli agentlog.CLI, cwd string, since time.Time) (agentlog.Ref, error) {
	if !agentTranscriptsOn() {
		return agentlog.Ref{}, errAgentTranscriptsOff
	}
	ref, ok := agentlogHomes().Latest(cli, cwd, since)
	if !ok {
		return agentlog.Ref{}, os.ErrNotExist
	}
	return ref, nil
}

func init() {
	aiSettingPolicies["agent_transcripts"] = aiNever("lets termocode read the outside agents' own session files; the user turns it on in config.json (owner decision D1)")
}
