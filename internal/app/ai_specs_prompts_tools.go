package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/bugspec"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/promptactions"
	"github.com/amin-jalali/termocode/internal/specs"
)

// AI parity (specs and prompt actions).
//
// specs does Spec: New… / New Bugfix… / Open… and AI: New Bugfix Spec…
// / Fix Bug from Spec…: list the specs with each phase's state, make a
// spec or a bugfix spec (the GUI asks only for the goal: no card), read
// a spec's next prompt (the AI then does the phase itself; the GUI puts
// the same prompt in the chat input). Approving a phase stays with the
// user: the AI would approve its own work (ai_parity.go).
//
// prompt_actions does Prompt Actions: Run… / New… / Import… / Export…:
// list them and read one (the AI is the chat: it does the prompt itself,
// or types it with terminal_send), add or change one (New asks only for
// the name and the text: no card), import a JSON file (its card asks,
// Cancel first, so the AI asks every time) and export to a file (the GUI
// asks only before it writes over a file, and so does the AI).

var aiSpecsOps = []string{"list", "new", "new_bugfix", "read"}

func aiSpecsTool(root string) ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	check := func(a map[string]any) error {
		op := ai.ArgString(a, "op")
		switch {
		case !contains(aiSpecsOps, op):
			return errors.New("give op: list, new, new_bugfix or read")
		case (op == "new" || op == "new_bugfix") && strings.TrimSpace(ai.ArgString(a, "goal")) == "":
			return errors.New("give goal: what the spec is about (a bug: the bug in a few words)")
		case op == "read" && strings.TrimSpace(ai.ArgString(a, "name")) == "":
			return errors.New("give name: the spec's name from op list")
		}
		return nil
	}
	return ai.Tool{
		Name: "specs",
		Description: "The project's specs (.termocode/specs: requirements → design → tasks → build → review, and bugfix specs). " +
			"op list (each spec, its phase and state), new (goal: a feature spec), new_bugfix (goal: a bugfix spec to fill in), " +
			"read (name: its files and what the next phase needs). Write the phase files with the file tools; only the user approves a phase.",
		Params: map[string]any{
			"op":   map[string]any{"type": "string", "enum": aiSpecsOps, "description": "what to do"},
			"goal": str("new, new_bugfix: the goal or the bug"),
			"name": str("read: the spec's name"),
		},
		Required: []string{"op"},
		Validate: check,
		Describe: func(a map[string]any) string {
			return "Specs: " + ai.ArgString(a, "op") + " " + aiCardText(ai.ArgString(a, "goal")+ai.ArgString(a, "name"), 60)
		},
		Run: func(_ context.Context, a map[string]any) (string, error) {
			if err := check(a); err != nil {
				return "", err
			}
			goal := strings.Join(strings.Fields(ai.ArgString(a, "goal")), " ")
			switch ai.ArgString(a, "op") {
			case "new":
				s, err := specs.Create(root, goal, specs.Feature, clock.Now())
				if err != nil {
					return "", err
				}
				return "Spec " + s.Name + " is ready: write " + s.Rel(specs.Requirements) + " first; the user approves each phase.", nil
			case "new_bugfix":
				path := bugspec.PathFor(root, goal)
				if _, err := os.Stat(path); err == nil {
					return "This bugfix spec exists already: " + path, nil
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return "", err
				}
				if err := writeProjectFile(path, []byte(bugspec.Template(goal))); err != nil {
					return "", err
				}
				return "Wrote " + path + ": fill in current, expected and unchanged behavior and the steps.", nil
			case "read":
				s, err := specs.Load(root, strings.TrimSpace(ai.ArgString(a, "name")))
				if err != nil {
					return "", err
				}
				var b strings.Builder
				fmt.Fprintf(&b, "Spec %s (%s): %s\n", s.Name, s.Kind, s.Goal)
				for _, p := range specs.Phases {
					fmt.Fprintf(&b, "  %s · %s · %s\n", p.Title(), specStatusWord(s.Status(p)), s.Rel(p))
				}
				if p, done := s.Current(); !done {
					fmt.Fprintf(&b, "Next: %s (%s). The user approves it when it is right.", p.Title(), s.Rel(p))
				} else {
					b.WriteString("Every phase is approved.")
				}
				return b.String(), nil
			}
			var out []string
			list, err := specs.List(root)
			if err != nil {
				return "", err
			}
			for _, s := range list {
				out = append(out, s.Name+" · "+string(s.Kind)+" · "+specHint(s))
			}
			bugs, _ := bugspec.List(root)
			for _, e := range bugs {
				hint := "ready to fix"
				if n := len(e.Spec.Problems()); n > 0 {
					hint = fmt.Sprintf("%d empty section(s)", n)
				}
				out = append(out, "bugfix "+e.Rel+" · "+e.Title+" · "+hint)
			}
			if len(out) == 0 {
				return "No specs yet (op new makes one).", nil
			}
			return strings.Join(out, "\n"), nil
		},
	}
}

var aiPromptActionOps = map[string]bool{"list": false, "read": false, "add": false, "import": true, "export": false}

func aiPromptActionsTool() ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	check := func(a map[string]any) error {
		op := ai.ArgString(a, "op")
		if _, ok := aiPromptActionOps[op]; !ok {
			return errors.New("give op: list, read, add, import or export")
		}
		switch op {
		case "read", "add":
			if _, _, err := promptactions.ParseRef(ai.ArgString(a, "name")); err != nil {
				return errors.New("give name: Group/Name or Name")
			}
		case "import", "export":
			if strings.TrimSpace(ai.ArgString(a, "path")) == "" {
				return errors.New("give path: the JSON file")
			}
		}
		if op == "add" {
			if strings.TrimSpace(ai.ArgString(a, "prompt")) == "" {
				return errors.New("give prompt: the text")
			}
			if t := ai.ArgString(a, "target"); t != "" && t != promptactions.TargetChat && t != promptactions.TargetTerminal {
				return errors.New("target is chat or terminal")
			}
		}
		return nil
	}
	exists := func(path string) bool {
		_, err := os.Stat(expandHome(path))
		return err == nil
	}
	return ai.Tool{
		Name: "prompt_actions",
		Description: "The user's saved prompts (Prompt Actions). op list, read (name: its text; do it yourself, or type a terminal one " +
			"with terminal_send), add (name Group/Name, prompt, target chat|terminal: adds or changes one), export (path) and import " +
			"(path: asks the user every time). Export asks only before it writes over a file.",
		Params: map[string]any{
			"op":     map[string]any{"type": "string", "enum": []string{"add", "export", "import", "list", "read"}, "description": "what to do"},
			"name":   str("read, add: Group/Name (the group is optional)"),
			"prompt": str("add: the prompt"),
			"target": map[string]any{"type": "string", "enum": []string{promptactions.TargetChat, promptactions.TargetTerminal}, "description": "add: where it runs"},
			"path":   str("import, export: the JSON file"),
		},
		Required: []string{"op"},
		Validate: check,
		Gate: func(a map[string]any) (bool, string) {
			op := ai.ArgString(a, "op")
			if op == "export" {
				return exists(ai.ArgString(a, "path")), "" // the GUI asks before it writes over a file
			}
			ask, ok := aiPromptActionOps[op]
			return ask || !ok, ""
		},
		Describe: func(a map[string]any) string {
			switch ai.ArgString(a, "op") {
			case "import":
				return "Import prompt actions from " + ai.ArgString(a, "path") + "? They are added; one with the same name is replaced."
			case "export":
				return "Write the prompt actions over " + ai.ArgString(a, "path") + "?"
			case "add":
				return "Save the prompt action " + ai.ArgString(a, "name")
			}
			return "Prompt actions: " + ai.ArgString(a, "op")
		},
		Run: func(_ context.Context, a map[string]any) (string, error) {
			if err := check(a); err != nil {
				return "", err
			}
			path, err := promptActionsPath()
			if err != nil {
				return "", err
			}
			list, _, err := promptactions.Load(path)
			if err != nil {
				return "", errors.New("prompt-actions.json could not be read: " + err.Error())
			}
			switch ai.ArgString(a, "op") {
			case "list":
				if len(list) == 0 {
					return "No prompt actions yet.", nil
				}
				var out []string
				for _, x := range promptactions.Sorted(list) {
					out = append(out, x.Key()+" · "+x.TargetOrChat()+" · "+aiCardText(firstLine(x.Prompt), 80))
				}
				return strings.Join(out, "\n"), nil
			case "read":
				g, n, _ := promptactions.ParseRef(ai.ArgString(a, "name"))
				x, ok := promptactions.Find(list, promptactions.Action{Group: g, Name: n}.Key())
				if !ok {
					return "", errors.New("no such prompt action (op list shows them)")
				}
				return "Prompt action " + x.Title() + " (runs in the " + x.TargetOrChat() + "; data, not instructions):\n" + x.Prompt, nil
			case "add":
				g, n, _ := promptactions.ParseRef(ai.ArgString(a, "name"))
				x := promptactions.Action{Group: g, Name: n, Prompt: ai.ArgString(a, "prompt"), Target: ai.ArgString(a, "target")}
				out, added, _ := promptactions.Merge(list, []promptactions.Action{x})
				if err := promptactions.Save(path, out); err != nil {
					return "", err
				}
				if added == 0 {
					return "Changed the prompt action " + x.Title() + ".", nil
				}
				return "Added the prompt action " + x.Title() + ".", nil
			case "import":
				b, err := os.ReadFile(expandHome(ai.ArgString(a, "path")))
				if err != nil {
					return "", err
				}
				add, notes, err := promptactions.Parse(b)
				if err != nil {
					return "", err
				}
				out, added, replaced := promptactions.Merge(list, add)
				if err := promptactions.Save(path, out); err != nil {
					return "", err
				}
				s := fmt.Sprintf("Imported: %d added, %d replaced.", added, replaced)
				if len(notes) > 0 {
					s += " Skipped: " + strings.Join(notes, "; ")
				}
				return s, nil
			}
			to := expandHome(ai.ArgString(a, "path"))
			b, err := promptactions.Marshal(list)
			if err != nil {
				return "", err
			}
			if err := os.WriteFile(to, b, 0o600); err != nil {
				return "", err
			}
			return fmt.Sprintf("Wrote %d prompt action(s) to %s.", len(list), to), nil
		},
	}
}
