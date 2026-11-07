package app

import (
	"strconv"

	"github.com/amin-jalali/termocode/internal/agentjail"
	"github.com/amin-jalali/termocode/internal/agents"
)

// CMP-60: agent_os_sandbox puts every outside agent lane inside an OS
// sandbox (internal/agentjail: bwrap on Linux, sandbox-exec on macOS):
// workflow agent steps, AI run CLI nodes and agent race lanes. The disk
// is read-only; only the lane's worktree, the temp folder and the
// agent's own config folders are writable. Off by default; only the
// user changes it (AI gate never). No sandbox tool: the lane does not
// start.

// raceJailHost is the machine the race lanes are sandboxed on (tests
// swap it).
var raceJailHost = agentjail.HostEnv

// agentOSSandboxOn reads the setting (a missing or bad value: off).
func agentOSSandboxOn() bool {
	v, _ := workflowSettingValue("setting-agent-os-sandbox")
	on, _ := strconv.ParseBool(v)
	return on
}

// raceAgentLine is what a race lane types into its terminal tab in dir:
// the agent's program, or with agent_os_sandbox on the sandbox line
// around it (note: the sandbox's state text for the lane's info line).
func raceAgentLine(a agents.Agent, dir string) (line, note string, err error) {
	if !agentOSSandboxOn() {
		return a.Bin, "", nil
	}
	h := raceJailHost()
	p := agentjail.Plan{Mode: agentjail.WorkspaceWrite, Dir: dir, Agent: a.ID, Interactive: true}
	line, err = agentjail.Line(p, h, a.Bin)
	if err != nil {
		return "", "", err
	}
	return line, agentjail.Note(p, h), nil
}
