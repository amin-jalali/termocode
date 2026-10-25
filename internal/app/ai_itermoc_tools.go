package app

import (
	"context"
	"encoding/json"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
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
	}
}
