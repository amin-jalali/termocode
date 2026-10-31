package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/amin-jalali/termocode/internal/agentctl"
)

// `termocode agent list|prompt|wait|report` (OMX-33, OMX-34): drive the
// agents in the terminal tabs of a running termocode over its control
// socket ($TERMOCODE_SOCKET inside a termocode terminal, else the socket
// of the current folder's workspace).

const agentUsage = `termocode agent: talk to the agents in termocode's terminal tabs

  termocode agent list [--json]
        the tabs: id, name, agent, state (working, waiting, done)
  termocode agent prompt <tab> <text…>   (text "-" reads stdin)
        type the text into the tab and press Enter (termocode asks you first)
  termocode agent wait <tab> [--until done|waiting|working] [--timeout 10m]
        wait until the tab's agent reaches the state (exit 1 on timeout)
  termocode agent report <working|waiting|done> [--agent claude|codex|…]
        an agent's hook says its own state ($TERMOCODE_TAB_ID is the tab)

Inside a termocode terminal: TERMOCODE_ENV=1, TERMOCODE_TAB_ID, TERMOCODE_SOCKET.
`

// agentCall is the socket call (a seam for tests).
var agentCall = agentctl.Call

func runAgent(args []string) int {
	return runAgentIn(args, os.Stdin, os.Stdout, os.Stderr, os.Getenv)
}

func agentSocket(getenv func(string) string) string {
	if p := getenv(agentctl.EnvSocket); p != "" {
		return p
	}
	return agentSocketOfCwd()
}

func runAgentIn(args []string, in io.Reader, out, errw io.Writer, getenv func(string) string) int {
	fail := func(f string, a ...any) int {
		fmt.Fprintf(errw, "termocode agent: "+f+"\n", a...)
		return 1
	}
	usage := func() int { fmt.Fprint(errw, agentUsage); return 2 }
	if len(args) == 0 {
		return usage()
	}
	sock := agentSocket(getenv)
	call := func(req agentctl.Request, timeout time.Duration) (agentctl.Reply, bool) {
		rep, err := agentCall(sock, req, timeout)
		if err != nil {
			fail("%v", err)
			return rep, false
		}
		return rep, true
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(out, agentUsage)
		return 0
	case agentctl.OpList:
		asJSON := len(args) > 1 && args[1] == "--json"
		rep, ok := call(agentctl.Request{Op: agentctl.OpList}, 10*time.Second)
		if !ok {
			return 1
		}
		if asJSON {
			_ = json.NewEncoder(out).Encode(rep.Tabs)
			return 0
		}
		tw := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tAGENT\tSTATE")
		for _, t := range rep.Tabs {
			name := t.Name
			if t.Active {
				name += " *"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", t.ID, name, dash(t.Agent), dash(t.State))
		}
		_ = tw.Flush()
		return 0
	case agentctl.OpPrompt:
		if len(args) < 3 {
			return usage()
		}
		text := strings.Join(args[2:], " ")
		if text == "-" {
			b, err := io.ReadAll(io.LimitReader(in, 64<<10))
			if err != nil {
				return fail("read stdin: %v", err)
			}
			text = string(b)
		}
		if _, ok := call(agentctl.Request{Op: agentctl.OpPrompt, Tab: args[1], Text: text}, 3*time.Minute); !ok {
			return 1
		}
		return 0
	case agentctl.OpWait:
		if len(args) < 2 {
			return usage()
		}
		req := agentctl.Request{Op: agentctl.OpWait, Tab: args[1], Until: "done"}
		timeout := 10 * time.Minute
		for i := 2; i < len(args); i++ {
			switch a := args[i]; {
			case a == "--until" && i+1 < len(args):
				i++
				req.Until = args[i]
			case strings.HasPrefix(a, "--until="):
				req.Until = strings.TrimPrefix(a, "--until=")
			case a == "--timeout" && i+1 < len(args):
				i++
				d, err := time.ParseDuration(args[i])
				if err != nil || d <= 0 {
					return fail("bad --timeout %q (like 90s or 10m)", args[i])
				}
				timeout = d
			default:
				return usage()
			}
		}
		if !agentctl.ValidState(req.Until) {
			return fail("--until must be one of %s", strings.Join(agentctl.States, ", "))
		}
		req.TimeoutS = int(timeout.Seconds())
		rep, ok := call(req, timeout+20*time.Second)
		if !ok {
			return 1
		}
		fmt.Fprintln(out, rep.State)
		return 0
	case agentctl.OpReport:
		if len(args) < 2 {
			return usage()
		}
		req := agentctl.Request{Op: agentctl.OpReport, Tab: getenv(agentctl.EnvTab), State: args[1]}
		for i := 2; i < len(args); i++ {
			switch a := args[i]; {
			case a == "--agent" && i+1 < len(args):
				i++
				req.Agent = args[i]
			case strings.HasPrefix(a, "--agent="):
				req.Agent = strings.TrimPrefix(a, "--agent=")
			case a == "--tab" && i+1 < len(args):
				i++
				req.Tab = args[i]
			default:
				return usage()
			}
		}
		if req.Tab == "" {
			return fail("no tab: run it inside a termocode terminal (TERMOCODE_TAB_ID) or pass --tab")
		}
		// A hook must never slow the agent down or fail it: a short
		// timeout, and a quiet exit 0 when termocode is not there.
		if _, err := agentCall(sock, req, 3*time.Second); err != nil && getenv("TERMOCODE_AGENT_DEBUG") != "" {
			fmt.Fprintf(errw, "termocode agent report: %v\n", err)
		}
		return 0
	}
	return usage()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
