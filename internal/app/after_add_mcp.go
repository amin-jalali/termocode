package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/afteradd"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// After add: an MCP server (after_add.go). When the user connects a
// server (the Connect a Service wizard, or an mcp_server_add the user
// allowed), these read-only checks run:
//
//   - connect: the handshake and how long it took;
//   - sign-in: whether the server still needs one (kept in the secret
//     store by the wizard);
//   - tools: how many, how risky by their annotations (no annotation is
//     risky), the token cost of their schemas (big: they stay on demand)
//     and names that overlap termocode's own tools;
//   - extras: its prompts and resources (usable as commands);
//   - links: a Postgres server and a saved Postgres connection, a GitHub
//     server and this repo's GitHub remote.
//
// The one suggestion: review the risky tools when there are any (their
// calls already ask first), else use its prompts as commands, else try
// it in the chat. No call to a tool of the server is ever made.

// afterAddMCPBigSchema is the schema token cost from which the card says
// the tools stay on demand.
const afterAddMCPBigSchema = 4000

// afterAddMCPProbe is how the checks read one server (tests fake it).
type afterAddMCPProbe struct {
	connect   func(ctx context.Context, name string) error
	known     func(name string) ([]mcp.Tool, bool)
	signIn    func(name string) bool // the server waits for a sign-in
	prompts   func(ctx context.Context, name string) (int, error)
	resources func(ctx context.Context, name string) (int, error)
	builtins  []string // termocode's own tool names
	links     func() (pgConns []string, remote string)
}

// afterAddMCPProbeFor is the probe of the Model (tests replace it).
var afterAddMCPProbeFor = func(m *Model) afterAddMCPProbe {
	mgr := m.aiMCP()
	root := aiWorkspaceRoot()
	return afterAddMCPProbe{
		connect: func(ctx context.Context, name string) error { return mgr.Connect(ctx, nil, name) },
		known: func(name string) ([]mcp.Tool, bool) {
			_, tools, ok := mgr.KnownTools(name)
			return tools, ok
		},
		signIn: func(name string) bool { return slices.Contains(mgr.NeedSignIn(), name) },
		prompts: func(ctx context.Context, name string) (int, error) {
			ps, err := mgr.Prompts(ctx, nil, name)
			return len(ps), err
		},
		resources: func(ctx context.Context, name string) (int, error) {
			rs, ts, err := mgr.Resources(ctx, nil, name)
			return len(rs) + len(ts), err
		},
		builtins: aiCoreTools,
		links:    func() ([]string, string) { return afterAddPGConns(root), afterAddRemote(root) },
	}
}

// afterAddPGConns are the names of the saved PostgreSQL connections (the
// user's file and the project's), never their secrets.
func afterAddPGConns(root string) []string {
	var files []string
	if dir, err := configDir(); err == nil {
		files = append(files, filepath.Join(dir, "db", "connections.json"))
	}
	files = append(files, filepath.Join(root, ".termocode", "databases.json"))
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		conns, _ := dbclient.ParseConfig(b, dbclient.SourceUser, f, root)
		for _, c := range conns {
			if c.Driver == "postgres" {
				name := c.Name
				if name == "" {
					name = c.ID
				}
				out = append(out, name)
			}
		}
	}
	return out
}

// afterAddRemote is this repo's web address ("" when none).
func afterAddRemote(root string) string {
	u, err := git.RemoteWeb(root)
	if err != nil {
		return ""
	}
	return u
}

// afterAddMCPEvent is the event of a server the user just added.
func afterAddMCPEvent(name string) afteradd.Event {
	return afteradd.Event{Kind: afteradd.KindMCP, Item: name}
}

// afterAddMCPAdded runs the checks of a newly added server.
func (m *Model) afterAddMCPAdded(name string) tea.Cmd {
	m.afterAddEngine().Add(afterAddMCPRule(afterAddMCPProbeFor(m)))
	return m.afterAdd(afterAddMCPEvent(name))
}

// afterAddMCPRule is the rule of KindMCP over probe p.
func afterAddMCPRule(p afterAddMCPProbe) afteradd.Rule {
	f := func(lv afteradd.Level, text string, facts ...string) afteradd.Finding {
		fs := map[string]string{}
		for i := 0; i+1 < len(facts); i += 2 {
			fs[facts[i]] = facts[i+1]
		}
		return afteradd.Finding{Level: lv, Text: text, Facts: fs}
	}
	return afteradd.Rule{Kind: afteradd.KindMCP,
		Checks: []afteradd.Check{
			{Name: "connect", Timeout: 10 * time.Second, Run: func(ctx context.Context, ev afteradd.Event) ([]afteradd.Finding, error) {
				start := clock.Now()
				if err := p.connect(ctx, ev.Item); err != nil {
					return nil, err
				}
				ms := clock.Now().Sub(start).Milliseconds()
				return []afteradd.Finding{f(afteradd.OK, fmt.Sprintf("connected, answered in %d ms", ms))}, nil
			}},
			{Name: "sign-in", Run: func(_ context.Context, ev afteradd.Event) ([]afteradd.Finding, error) {
				if p.signIn(ev.Item) {
					return []afteradd.Finding{f(afteradd.Warn, "it needs a sign-in (kept in the secret store)", "signin", "1")}, nil
				}
				return nil, nil
			}},
			{Name: "tools", Run: func(_ context.Context, ev afteradd.Event) ([]afteradd.Finding, error) {
				tools, _ := p.known(ev.Item)
				return afterAddMCPToolFindings(tools, p.builtins, f), nil
			}},
			{Name: "extras", Run: func(ctx context.Context, ev afteradd.Event) ([]afteradd.Finding, error) {
				np, _ := p.prompts(ctx, ev.Item)
				nr, _ := p.resources(ctx, ev.Item)
				if np+nr == 0 {
					return nil, nil
				}
				return []afteradd.Finding{f(afteradd.Info, fmt.Sprintf("%d prompts and %d resources: usable as commands", np, nr), "prompts", strconv.Itoa(np+nr))}, nil
			}},
			{Name: "links", Run: func(_ context.Context, ev afteradd.Event) ([]afteradd.Finding, error) {
				return afterAddMCPLinks(ev.Item, p.links, f), nil
			}},
		},
		Suggest: afterAddMCPSuggest,
	}
}

// afterAddMCPToolFindings reads the tool list: count and risk, schema
// cost, overlaps.
func afterAddMCPToolFindings(tools []mcp.Tool, builtins []string, f func(afteradd.Level, string, ...string) afteradd.Finding) []afteradd.Finding {
	if len(tools) == 0 {
		return []afteradd.Finding{f(afteradd.Warn, "it lists no tools")}
	}
	var read, write, unknown int
	cost := 0
	var overlap []string
	for _, t := range tools {
		switch {
		case t.Annotations == nil || t.Annotations.ReadOnlyHint == nil && t.Annotations.DestructiveHint == nil:
			unknown++
		case t.ReadOnly():
			read++
		default:
			write++
		}
		b, _ := json.Marshal(t.InputSchema)
		cost += (len(t.Name) + len(t.Description) + len(b)) / 4
		if slices.Contains(builtins, t.Name) {
			overlap = append(overlap, t.Name)
		}
	}
	lv := afteradd.OK
	if write+unknown > 0 {
		lv = afteradd.Warn
	}
	out := []afteradd.Finding{f(lv, fmt.Sprintf("%d tools: %d read-only, %d write, %d unknown (unknown counts as risky; their calls ask first)", len(tools), read, write, unknown),
		"risky", strconv.Itoa(write+unknown))}
	size := fmt.Sprintf("tool schemas cost about %d tokens", cost)
	if cost >= afterAddMCPBigSchema {
		out = append(out, f(afteradd.Info, size+": big, so they stay on demand (tool search)"))
	} else {
		out = append(out, f(afteradd.Info, size))
	}
	if len(overlap) > 0 {
		out = append(out, f(afteradd.Warn, "same names as termocode's own tools: "+strings.Join(overlap, ", ")))
	}
	return out
}

// afterAddMCPLinks links the server to what termocode already has.
func afterAddMCPLinks(name string, links func() ([]string, string), f func(afteradd.Level, string, ...string) afteradd.Finding) []afteradd.Finding {
	low := strings.ToLower(name)
	pg := strings.Contains(low, "postgres") || low == "pg"
	gh := strings.Contains(low, "github")
	if !pg && !gh {
		return nil
	}
	conns, remote := links()
	var out []afteradd.Finding
	if pg && len(conns) > 0 {
		out = append(out, f(afteradd.Info, "you already have the PostgreSQL connection "+strings.Join(conns, ", ")+" in Databases"))
	}
	if gh && strings.Contains(remote, "github.com/") {
		repo := remote[strings.Index(remote, "github.com/")+len("github.com/"):]
		out = append(out, f(afteradd.Info, "this project's repo is "+repo+" on GitHub: ask about its issues and pull requests"))
	}
	return out
}

// afterAddMCPSuggest picks the one next step.
func afterAddMCPSuggest(ev afteradd.Event, fs []afteradd.Finding) *afteradd.Suggestion {
	risky, extras := 0, 0
	for _, x := range fs {
		if n, err := strconv.Atoi(x.Facts["risky"]); err == nil {
			risky = n
		}
		if x.Facts["prompts"] != "" {
			extras++
		}
	}
	title := "Connected " + ev.Item
	switch {
	case risky > 0:
		return &afteradd.Suggestion{Title: title + ": review its tools?", Action: afteradd.Action{ID: "review-tools", Label: "Review tools", Op: "ai-mcp-servers"}}
	case extras > 0:
		return &afteradd.Suggestion{Title: title + ": use its prompts?", Action: afteradd.Action{ID: "prompts", Label: "Open prompts", Op: "ai-mcp-insert"}}
	}
	return &afteradd.Suggestion{Title: title + ": try it in the chat?", Action: afteradd.Action{ID: "chat", Label: "Open the chat", Op: "ai-open-chat"}}
}
