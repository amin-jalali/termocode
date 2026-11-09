package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// view_manage: the AI does what the phone's saved views do (AI parity,
// the web's view.list, view.rename, view.pin and view.delete): list the
// saved views, rename one, pin it to Home or unpin it, and delete it.
// Delete asks every time, as the page asks (Cancel first); the others
// have no card there and run at once. view_save saves a new one.

var aiViewManageOps = map[string]bool{"list": false, "rename": false, "pin": false, "unpin": false, "delete": true}

func aiViewManageCheck(a map[string]any) error {
	op := ai.ArgString(a, "op")
	if _, ok := aiViewManageOps[op]; !ok {
		return errors.New("give op: list, rename, pin, unpin or delete")
	}
	if op != "list" && strings.TrimSpace(ai.ArgString(a, "id")) == "" {
		return errors.New("give id: the view id from op list")
	}
	if op == "rename" {
		if n := strings.TrimSpace(ai.ArgString(a, "name")); n == "" || len([]rune(n)) > webproto.MaxViewName {
			return fmt.Errorf("give name: up to %d characters", webproto.MaxViewName)
		}
	}
	return nil
}

func aiViewManageTool(call aiIDECaller) ai.Tool {
	return ai.Tool{
		Name: "view_manage",
		Description: "The views saved for the phones (view_save saves one). op list, rename (id, name), pin (id: to the " +
			"phones' Home) and unpin (id) run at once; delete (id) asks the user every time.",
		Params: map[string]any{
			"op":   map[string]any{"type": "string", "enum": []string{"delete", "list", "pin", "rename", "unpin"}, "description": "what to do"},
			"id":   map[string]any{"type": "string", "description": "the view id from op list"},
			"name": map[string]any{"type": "string", "description": "rename: the new name"},
		},
		Required: []string{"op"},
		Validate: aiViewManageCheck,
		Describe: func(a map[string]any) string {
			id := strings.TrimSpace(ai.ArgString(a, "id"))
			name := id
			if vs, err := loadSavedViews(); err == nil {
				if i := savedViewIndex(vs, id); i >= 0 {
					name = vs[i].Name
				}
			}
			switch ai.ArgString(a, "op") {
			case "delete":
				return fmt.Sprintf("Delete the saved view %q? Every paired phone loses it.", name)
			case "rename":
				return fmt.Sprintf("Rename the view %q to %q", name, strings.TrimSpace(ai.ArgString(a, "name")))
			case "pin", "unpin":
				return ai.ArgString(a, "op") + " the view " + fmt.Sprintf("%q", name)
			}
			return "List the saved views"
		},
		Gate: func(a map[string]any) (bool, string) {
			ask, ok := aiViewManageOps[ai.ArgString(a, "op")]
			return ask || !ok, ""
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			if err := aiViewManageCheck(a); err != nil {
				return "", err
			}
			if call == nil {
				return "", errors.New("views are kept on the laptop")
			}
			op, id, name := ai.ArgString(a, "op"), strings.TrimSpace(ai.ArgString(a, "id")), strings.TrimSpace(ai.ArgString(a, "name"))
			return call(ctx, func(m *Model) (string, tea.Cmd, error) {
				vs, err := loadSavedViews()
				if err != nil {
					return "", nil, errors.New("views.json on the laptop is not valid JSON")
				}
				if op == "list" {
					if len(vs) == 0 {
						return "No saved views.", nil, nil
					}
					var out []string
					for _, v := range vs {
						s := fmt.Sprintf("%s · %q · %s", v.ID, v.Name, v.Spec.View)
						if v.Pinned {
							s += fmt.Sprintf(" · pinned #%d", v.Order+1)
						}
						if v.By != "" {
							s += " · by " + v.By
						}
						out = append(out, s)
					}
					return strings.Join(out, "\n"), nil, nil
				}
				i := savedViewIndex(vs, id)
				if i < 0 {
					return "", nil, errors.New("that view is not there any more (op list shows them)")
				}
				was := vs[i].Name
				var done string
				switch op {
				case "rename":
					if j := savedViewByName(vs, name); j >= 0 && j != i {
						return "", nil, errors.New("another view has that name")
					}
					vs[i].Name, vs[i].Saved = name, webNow().UnixMilli()
					sortSavedViews(vs)
					done = fmt.Sprintf("Renamed %q to %q.", was, name)
				case "pin":
					if vs[i].Pinned {
						return "", nil, errors.New("it is pinned already")
					}
					if pinnedCount(vs) >= webproto.MaxPinnedViews {
						return "", nil, fmt.Errorf("%d views are pinned to Home already; unpin one first", webproto.MaxPinnedViews)
					}
					pinAt(vs, id, 0)
					done = fmt.Sprintf("Pinned %q to the Home of every paired phone.", was)
				case "unpin":
					if !vs[i].Pinned {
						return "", nil, errors.New("it is not pinned")
					}
					vs[i].Pinned, vs[i].Order = false, 0
					renumberPins(vs)
					done = fmt.Sprintf("Unpinned %q.", was)
				case "delete":
					vs = append(vs[:i], vs[i+1:]...)
					renumberPins(vs)
					done = fmt.Sprintf("Deleted the view %q.", was)
				}
				if err := saveSavedViews(vs); err != nil {
					recordError("[web] views.json: " + err.Error())
					return "", nil, errors.New("views.json could not be written")
				}
				m.web.sendViewsSaved(vs)
				return done, nil, nil
			})
		},
	}
}
