package app

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// aiContainersGUIActions are the servers, containers and Kubernetes
// actions of the GUI: the palette ids containers-*, kubernetes-* and
// services-deploy-git, and the web's containers.* / kube.* ops and
// db.add_container.
func aiContainersGUIActions() []string {
	var out []string
	for _, d := range AIActionPolicyCatalog() {
		if strings.HasPrefix(d.ID, "containers-") || strings.HasPrefix(d.ID, "kubernetes-") || d.ID == "services-deploy-git" {
			out = append(out, d.ID)
		}
	}
	for _, p := range webproto.Ops() {
		if strings.HasPrefix(p.Op, "containers.") || strings.HasPrefix(p.Op, "kube.") || p.Op == webproto.OpDBAddContainer {
			out = append(out, p.Op)
		}
	}
	sort.Strings(out)
	return out
}

// aiParityOps are the ops a parity value names: "tool op a / b".
func aiParityOps(v string) (tool, arg string, ops []string) {
	f := strings.Fields(v)
	if len(f) == 0 {
		return "", "", nil
	}
	tool = f[0]
	if len(f) > 2 && (f[1] == "op" || f[1] == "action" || f[1] == "kind") {
		arg = f[1]
		for _, w := range f[2:] {
			if w != "/" {
				ops = append(ops, w)
			}
		}
	}
	return tool, arg, ops
}

// Guard (containers parity): every servers, containers and Kubernetes
// action of the GUI maps to an AI tool that does it (aiContainersParity)
// or sits on the commented "not for AI" list (aiContainersNotForAI). A new
// palette id or web op of these areas with neither makes this test red.
// The tools named must be chat tools, every op named must be one the tool
// takes, and an execute_action row must be one execute_action may run.
func TestAIContainersEveryGUIActionHasAnAITool(t *testing.T) {
	tools := map[string]ai.Tool{}
	m := &Model{services: newServicesState()}
	for _, tl := range m.aiChatTools(t.TempDir(), func(context.Context, aiIDEFunc) (string, error) { return "", nil }) {
		tools[tl.Name] = tl
	}
	for _, d := range AIToolCatalog() {
		if _, ok := tools[d.Name]; !ok {
			tools[d.Name] = ai.Tool{Name: d.Name}
		}
	}
	actions := aiContainersGUIActions()
	if len(actions) < 140 {
		t.Fatalf("only %d actions found; the lookup is broken", len(actions))
	}
	known := map[string]bool{}
	var missing []string
	for _, id := range actions {
		known[id] = true
		v, mapped := aiContainersParity[id]
		why, skipped := aiContainersNotForAI[id]
		switch {
		case mapped && skipped:
			t.Errorf("%s is on both lists", id)
		case !mapped && !skipped:
			missing = append(missing, id)
		case skipped && strings.TrimSpace(why) == "":
			t.Errorf("%s is not for AI with no reason", id)
		case mapped:
			name, arg, ops := aiParityOps(v)
			tl, ok := tools[name]
			if !ok {
				t.Errorf("%s maps to %q, which is not a chat tool", id, v)
				continue
			}
			if name == "execute_action" {
				if p, ok := aiActionPolicyFor(id); !ok || p.Gate == aiGateNever {
					t.Errorf("%s maps to execute_action, which never runs it", id)
				}
			}
			if arg != "" {
				enum := aiParamEnum(tl.Params[arg])
				for _, op := range ops {
					if !contains(enum, op) {
						t.Errorf("%s maps to %s %s %s, which it does not take (%v)", id, name, arg, op, enum)
					}
				}
			}
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d actions map to no AI tool and are not on the not-for-AI list:\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
	for _, mp := range []map[string]string{aiContainersParity, aiContainersNotForAI} {
		for id := range mp {
			if !known[id] {
				t.Errorf("%s is listed but is no servers, containers or Kubernetes action of the GUI", id)
			}
		}
	}
}
