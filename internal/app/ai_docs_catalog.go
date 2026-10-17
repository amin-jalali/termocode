package app

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// K8: catalogs for the AI tools reference page (cmd/docgen →
// docs/reference/ai-tools.md). They are read from the live tool list
// (aiChatTools) and the policy maps, so the page can not fall behind.

// AIToolArg is one argument of an AI tool.
type AIToolArg struct {
	Name        string
	Type        string
	Description string
	Required    bool
	Enum        []string
}

// AIToolDoc is one AI tool: what it does, its arguments and its gate.
type AIToolDoc struct {
	Name        string
	Description string
	Args        []AIToolArg
	// Gate is "auto", "confirm (<class>)", "confirm (always asks)", or
	// one of these per op ("list: auto; install: confirm (run)").
	Gate string
}

// AIPolicyDoc is the AI policy of one palette action or setting.
type AIPolicyDoc struct {
	ID     string
	Title  string // palette title ("" for a setting)
	Gate   string // auto | confirm | never
	Class  string // confirm: the "Always allow" class; "" always asks
	Reason string
}

// aiGateNotes replace the computed gate of tools whose gate depends on
// more than one argument value.
var aiGateNotes = map[string]string{
	"execute_action": "per action: see Palette actions below",
	"http_send":      "GET / HEAD / OPTIONS to localhost: auto; other safe methods: confirm (http:<host[:port]>); other methods: confirm (http:<host[:port]>/write); private, link-local or metadata hosts, *.internal, *.local and dot-less names: confirm (always asks)",
	"terminal_tabs":  "list, new, focus: auto; close: auto for an idle tab, confirm (always asks) while a command runs",
}

// aiGateRefused are ops a tool always refuses (Validate), with no dialog.
var aiGateRefused = map[string]map[string]bool{
	"workspace_folders": {"open": true},
}

// aiGateText is the gate of one call.
func aiGateText(mutating bool, class string) string {
	switch {
	case !mutating:
		return "auto"
	case class == "":
		return "confirm (always asks)"
	}
	return "confirm (" + class + ")"
}

// aiToolGate is the gate of t, per op when it differs by an enum argument.
func aiToolGate(t ai.Tool) string {
	if n, ok := aiGateNotes[t.Name]; ok {
		return n
	}
	if t.Gate == nil {
		return aiGateText(t.Mutating, t.Class)
	}
	keys := make([]string, 0, len(t.Params))
	for k := range t.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		enum := aiParamEnum(t.Params[k])
		if len(enum) == 0 {
			continue
		}
		var parts []string
		seen := map[string]bool{}
		for _, v := range enum {
			g := aiGateText(t.Gate(map[string]any{k: v}))
			if aiGateRefused[t.Name][v] {
				g = "never (refused)"
			}
			seen[g] = true
			parts = append(parts, v+": "+g)
		}
		if len(seen) > 1 {
			return strings.Join(parts, "; ")
		}
	}
	return aiGateText(t.Gate(map[string]any{}))
}

func aiParamEnum(p any) []string {
	m, ok := p.(map[string]any)
	if !ok {
		return nil
	}
	switch e := m["enum"].(type) {
	case []string:
		return e
	case []any:
		var out []string
		for _, v := range e {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// AIToolCatalog returns every AI chat tool in the order the model sees
// them. The tools are built (never run) with a stand-in editor.
func AIToolCatalog() []AIToolDoc {
	m := &Model{nvim: &nvim.Client{}}
	call := func(context.Context, aiIDEFunc) (string, error) { return "", errors.New("docs only") }
	tools := m.aiChatTools(".", call)
	out := make([]AIToolDoc, 0, len(tools))
	for _, t := range tools {
		d := AIToolDoc{Name: t.Name, Description: t.Description, Gate: aiToolGate(t)}
		req := map[string]bool{}
		for _, r := range t.Required {
			req[r] = true
		}
		for name, p := range t.Params {
			a := AIToolArg{Name: name, Required: req[name], Enum: aiParamEnum(p)}
			if pm, ok := p.(map[string]any); ok {
				a.Type, _ = pm["type"].(string)
				a.Description, _ = pm["description"].(string)
			}
			d.Args = append(d.Args, a)
		}
		sort.Slice(d.Args, func(i, j int) bool {
			if d.Args[i].Required != d.Args[j].Required {
				return d.Args[i].Required
			}
			return d.Args[i].Name < d.Args[j].Name
		})
		out = append(out, d)
	}
	return out
}

func aiPolicyDoc(id, title string, p aiActionPolicy) AIPolicyDoc {
	return AIPolicyDoc{ID: id, Title: title, Gate: string(p.Gate), Class: p.Class, Reason: p.Reason}
}

// AIActionPolicyCatalog returns the policy of every static palette
// command (what execute_action may run), sorted by id.
func AIActionPolicyCatalog() []AIPolicyDoc {
	var out []AIPolicyDoc
	seen := map[string]bool{}
	for _, it := range append(paletteItems(), tasksPaletteItems()...) {
		if seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		if p, ok := aiActionPolicyFor(it.ID); ok {
			out = append(out, aiPolicyDoc(it.ID, it.Title, p))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// AISettingPolicyCatalog returns the policy of every setting (what
// settings_set may change), sorted by key.
func AISettingPolicyCatalog() []AIPolicyDoc {
	out := make([]AIPolicyDoc, 0, len(aiSettingPolicies))
	for k, p := range aiSettingPolicies {
		out = append(out, aiPolicyDoc(k, "", p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
