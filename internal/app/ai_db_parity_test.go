package app

import (
	"sort"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// aiDBGUIActions are the database actions of the GUI: the palette ids of
// the Databases view (db-*, redis-*, and Open in Databases of the
// Containers view) and the web's db.* / redis.* ops (and containers.open_db).
func aiDBGUIActions() []string {
	var out []string
	for _, d := range AIActionPolicyCatalog() {
		if strings.HasPrefix(d.ID, "db-") || strings.HasPrefix(d.ID, "redis-") || d.ID == "containers-open-databases" {
			out = append(out, d.ID)
		}
	}
	for _, p := range webproto.Ops() {
		if strings.HasPrefix(p.Op, "db.") || strings.HasPrefix(p.Op, "redis.") || p.Op == webproto.OpContainersOpenDB {
			out = append(out, p.Op)
		}
	}
	sort.Strings(out)
	return out
}

// Guard (DB parity): every database action of the GUI maps to an AI tool
// that does it (aiDBParity) or sits on the commented "not for AI" list
// (aiDBNotForAI). A new palette id or web op of the Databases area with
// neither makes this test red. The tools named must exist in the chat; an
// execute_action row must be one execute_action may run.
func TestAIDBEveryGUIActionHasAnAITool(t *testing.T) {
	tools := map[string]bool{}
	for _, d := range AIToolCatalog() {
		tools[d.Name] = true
	}
	actions := aiDBGUIActions()
	if len(actions) < 100 {
		t.Fatalf("only %d database actions found; the lookup is broken", len(actions))
	}
	known := map[string]bool{}
	var missing []string
	for _, id := range actions {
		known[id] = true
		tool, mapped := aiDBParity[id]
		why, skipped := aiDBNotForAI[id]
		switch {
		case mapped && skipped:
			t.Errorf("%s is on both lists", id)
		case !mapped && !skipped:
			missing = append(missing, id)
		case skipped && strings.TrimSpace(why) == "":
			t.Errorf("%s is not for AI with no reason", id)
		case mapped:
			name, _, _ := strings.Cut(tool, " ")
			if !tools[name] {
				t.Errorf("%s maps to %q, which is not a chat tool", id, tool)
			}
			if name == "execute_action" {
				if p, ok := aiActionPolicyFor(id); !ok || p.Gate == aiGateNever {
					t.Errorf("%s maps to execute_action, which never runs it", id)
				}
			}
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d database actions map to no AI tool and are not on the not-for-AI list:\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
	for _, m := range []map[string]string{aiDBParity, aiDBNotForAI} {
		for id := range m {
			if !known[id] {
				t.Errorf("%s is listed but is no database action of the GUI", id)
			}
		}
	}
}
