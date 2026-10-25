package app

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// ── AI read tools for the Itermoc view (AH-7) ─────────────────────────────
//
// The chat can answer "what does the review profile do?", "which models
// do I have?", "what is connected?" and "who is using termocode?" from
// data. Every tool reads the same snapshot the view draws (itermocBuild)
// and runs at once: nothing changes. What they never hold: key values,
// tokens, headers, env values, URLs with a user, password or query, and
// more of a prompt than its first line. The JSON passes ai.Redact (the
// key patterns) here and ai.RedactSecrets (every known secret) in
// aiChatTools. Outside agents (termocode as an MCP server) do not get
// these tools: they describe the chat's own setup (mcpServeAllowList).

// aiItermocJSON is a tool answer: indented JSON with the key patterns
// masked.
func aiItermocJSON(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return ai.Redact(string(b)), nil
}

// aiItermocSnapshot reads the view's state again on the UI goroutine.
func aiItermocSnapshot(ctx context.Context, call aiIDECaller, build func(m *Model, sn itermocSnap) (any, error)) (string, error) {
	return call(ctx, func(m *Model) (string, tea.Cmd, error) {
		v, err := build(m, itermocBuild(*m, true))
		if err != nil {
			return "", nil, err
		}
		out, err := aiItermocJSON(v)
		return out, nil, err
	})
}

// ── profiles_list ────────────────────────────────────────────────────────

type aiProfileStepInfo struct {
	ID       string   `json:"id"`
	Title    string   `json:"title,omitempty"`
	Slot     string   `json:"slot"`
	Category string   `json:"category,omitempty"`
	Model    string   `json:"model"`
	Tools    string   `json:"tools"`
	Allow    []string `json:"tools_allow,omitempty"`
	Deny     []string `json:"tools_deny,omitempty"`
	Plan     bool     `json:"planner,omitempty"`
	Deps     []string `json:"after,omitempty"`
	Prompt   string   `json:"prompt_first_line,omitempty"`
}

type aiProfileInfo struct {
	Name       string              `json:"name"`
	Title      string              `json:"title,omitempty"`
	Source     string              `json:"source"`
	Trust      string              `json:"trust,omitempty"`
	Active     bool                `json:"active"`
	BudgetUSD  float64             `json:"budget_usd,omitempty"`
	Categories map[string]string   `json:"categories,omitempty"`
	Steps      []aiProfileStepInfo `json:"steps"`
}

// aiProfileSourceText names where a profile comes from.
func aiProfileSourceText(src string) string {
	switch src {
	case ai.ProfileSourceBuiltin, "":
		return "built-in"
	case ai.ProfileSourceUser:
		return "user (profiles.json)"
	}
	return src
}

// aiProfileTrustText is the trust state of a project profile.
func aiProfileTrustText(trust string) string {
	switch trust {
	case aiTrustUse:
		return "trusted"
	case aiTrustRefused:
		return "refused (not in this project)"
	}
	return "not asked yet"
}

// aiFirstLine is the first non-empty line of a prompt, cut to 160 runes.
func aiFirstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if r := []rune(l); len(r) > 160 {
				return string(r[:159]) + "…"
			}
			return l
		}
	}
	return ""
}

func aiProfilesInfo(m *Model, sn itermocSnap) (any, error) {
	out := struct {
		Active   string          `json:"active"`
		Profiles []aiProfileInfo `json:"profiles"`
	}{Profiles: []aiProfileInfo{}}
	for _, ip := range sn.profiles {
		p := ip.p
		info := aiProfileInfo{Name: p.Name, Title: p.Title, Source: aiProfileSourceText(p.Source), Active: ip.active,
			BudgetUSD: p.BudgetUSD, Categories: p.Categories, Steps: []aiProfileStepInfo{}}
		if p.Source == ai.ProfileSourceProject {
			info.Source = "project"
			info.Trust = aiProfileTrustText(ip.trust)
		}
		if ip.active {
			out.Active = p.Name
		}
		for _, st := range p.Steps {
			slot := p.SlotFor(st)
			model := ""
			if m.ai != nil {
				model, _ = m.ai.aiSlotModel(slot)
			}
			preset := st.Tools.Preset
			if preset == "" {
				preset = "all"
			}
			info.Steps = append(info.Steps, aiProfileStepInfo{ID: st.ID, Title: st.Title, Slot: slot, Category: st.Category,
				Model: model, Tools: preset, Allow: st.Tools.Allow, Deny: st.Tools.Deny, Plan: st.Plan, Deps: st.Deps,
				Prompt: aiFirstLine(st.System)})
		}
		out.Profiles = append(out.Profiles, info)
	}
	return out, nil
}

// ── models_list ──────────────────────────────────────────────────────────

type aiModelSlotInfo struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// Credential says whether a key is set: set, missing, not needed, or
	// none (optional). Never the key.
	Credential string `json:"credential"`
	Base       string `json:"base_host,omitempty"`
}

// aiCredentialText says what a slot has for its key, never the key.
func aiCredentialText(k imKeyState) string {
	switch k {
	case imKeyOK:
		return "set"
	case imKeyNone:
		return "not needed"
	case imKeyUnset:
		return "none (optional)"
	}
	return "missing"
}

// aiSafeHost is scheme://host[:port] of a URL: no user, password, path,
// query or fragment (a proxy may carry a key in any of them). "" when it
// does not parse.
func aiSafeHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.Scheme == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func aiModelsInfo(m *Model, sn itermocSnap) (any, error) {
	out := struct {
		Slots []aiModelSlotInfo `json:"slots"`
	}{Slots: []aiModelSlotInfo{}}
	for _, s := range sn.slots {
		info := aiModelSlotInfo{Name: s.name, Provider: s.provider, Model: s.model, Credential: aiCredentialText(s.key)}
		if m.ai != nil {
			if c, ok := m.ai.aiSlotConfig(s.name); ok {
				info.Base = aiSafeHost(c.BaseURL)
			}
		}
		out.Slots = append(out.Slots, info)
	}
	return out, nil
}

// ── mcp_services_list ────────────────────────────────────────────────────

type aiMCPServiceInfo struct {
	Name         string `json:"name"`
	State        string `json:"state"`
	Problem      string `json:"problem,omitempty"`
	Transport    string `json:"transport"`
	Source       string `json:"source"`
	Trusted      bool   `json:"trusted"`
	Access       string `json:"access"`
	Tools        int    `json:"tools"`
	ToolsChanged bool   `json:"tools_changed,omitempty"`
	SignedIn     bool   `json:"signed_in"`
	Expired      bool   `json:"sign_in_expired,omitempty"`
	AutoApproved int    `json:"auto_approved_tools,omitempty"`
}

var aiURLInText = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.\-]*://[^\s"'<>]+`)

// aiCleanURLs cuts every URL in s to scheme://host (a problem text may
// quote the server URL with its user, password or token).
func aiCleanURLs(s string) string {
	return aiURLInText.ReplaceAllStringFunc(s, func(u string) string {
		trimmed := strings.TrimRight(u, ".,;:)]}")
		tail := u[len(trimmed):]
		if h := aiSafeHost(trimmed); h != "" {
			return h + tail
		}
		return "[url]" + tail
	})
}

// aiMCPAccess says whether a service's tools may write: its readOnly
// flag, or for a known service (GitHub, Linear …) its read-only endpoint
// or scopes.
func aiMCPAccess(cfg mcp.Server) string {
	if cfg.ReadOnly {
		return "read only"
	}
	if p, ok := mcpPresetFor(cfg.Name); ok && mcpEntryReadOnly(cfg, p) {
		return "read only"
	}
	return "read and write"
}

func aiMCPServicesInfo(m *Model, sn itermocSnap) (any, error) {
	out := struct {
		Services []aiMCPServiceInfo `json:"services"`
	}{Services: []aiMCPServiceInfo{}}
	var mgr *mcp.Manager
	if m.ai != nil {
		mgr = m.ai.mcp
	}
	for _, s := range sn.services {
		info := aiMCPServiceInfo{Name: s.Name, State: s.State, Problem: aiCleanURLs(s.Problem), Transport: s.Type,
			Source: s.Source, Trusted: s.Trusted, Tools: s.Tools, ToolsChanged: s.ToolsChanged,
			SignedIn: s.SignedIn, Expired: s.Expired, Access: "read and write"}
		if mgr != nil {
			if cfg, _, _ := mgr.KnownTools(s.Name); cfg.Name != "" { // the bool is "tools known", not "found"
				info.Access = aiMCPAccess(cfg)
				info.AutoApproved = len(cfg.AutoApprove)
			}
		}
		out.Services = append(out.Services, info)
	}
	return out, nil
}

// ── outside_agents_status ────────────────────────────────────────────────

type aiOutsideAgentInfo struct {
	Name  string `json:"name"`
	Since string `json:"since"`
	Calls int    `json:"calls"`
	Asked int    `json:"asked"`
}

func aiOutsideAgentsInfo(m *Model, sn itermocSnap) (any, error) {
	out := struct {
		Mode      string               `json:"mode"`
		Listening bool                 `json:"listening"`
		Why       string               `json:"not_listening_why,omitempty"`
		Agents    []aiOutsideAgentInfo `json:"agents"`
		Calls     []string             `json:"recent_calls"`
		Always    map[string][]string  `json:"always_allowed"`
	}{Mode: sn.mode, Listening: sn.listening, Why: sn.whySentence, Agents: []aiOutsideAgentInfo{},
		Calls: []string{}, Always: map[string][]string{}}
	for _, a := range sn.agents {
		out.Agents = append(out.Agents, aiOutsideAgentInfo{Name: a.name, Since: a.since, Calls: a.calls, Asked: a.asked})
	}
	for _, l := range m.mcpServeAuditLines(10) {
		if row, ok := mcpServeCallRow(l); ok {
			out.Calls = append(out.Calls, aiCleanURLs(row))
		}
	}
	for client, keys := range mcpServeAllowLoad() {
		out.Always[client] = append([]string(nil), keys...)
	}
	return out, nil
}

// ── The tools ────────────────────────────────────────────────────────────

// aiItermocTools are the read tools of the Itermoc view.
func aiItermocTools(call aiIDECaller) []ai.Tool {
	return []ai.Tool{
		{
			Name: "profiles_list",
			Description: "List the AI agent profiles: name, title, source (built-in, user, project with its trust state), " +
				"which is active, budget, and each step's title, model slot, resolved model, tools preset and the first " +
				"line of its prompt. Use it to answer what a profile does; switching stays with the user.",
			Params:   map[string]any{},
			Describe: func(map[string]any) string { return "List AI profiles" },
			Run: func(ctx context.Context, _ map[string]any) (string, error) {
				return aiItermocSnapshot(ctx, call, aiProfilesInfo)
			},
		},
		{
			Name: "models_list",
			Description: "List the AI model slots (main, quick, local and the user's own): provider, model, whether a key " +
				"is set (set / missing / not needed; never the key) and the base URL's scheme and host. Keys stay with the user.",
			Params:   map[string]any{},
			Describe: func(map[string]any) string { return "List model slots" },
			Run: func(ctx context.Context, _ map[string]any) (string, error) {
				return aiItermocSnapshot(ctx, call, aiModelsInfo)
			},
		},
		{
			Name: "mcp_services_list",
			Description: "List the MCP services the chat can use (GitHub, Linear, the user's own servers in mcp.json): " +
				"state (running, stopped, off, failed, sign-in …), transport, source and trust, access (read only or read and " +
				"write), tool count and whether a sign-in is stored. Never tokens, headers, env or secret values. " +
				"Connecting and signing in stay with the user. (services_list is the project's dev services.)",
			Params:   map[string]any{},
			Describe: func(map[string]any) string { return "List MCP services" },
			Run: func(ctx context.Context, _ map[string]any) (string, error) {
				return aiItermocSnapshot(ctx, call, aiMCPServicesInfo)
			},
		},
		{
			Name: "outside_agents_status",
			Description: "Whether termocode's MCP server listens for outside agents (Claude Code, Codex …), and why not; " +
				"the connected agents with since, calls and how many asked; the last 10 calls; and the tools each agent " +
				"may run without asking. Revoking access stays with the user.",
			Params:   map[string]any{},
			Describe: func(map[string]any) string { return "Outside agents status" },
			Run: func(ctx context.Context, _ map[string]any) (string, error) {
				return aiItermocSnapshot(ctx, call, aiOutsideAgentsInfo)
			},
		},
	}
}
