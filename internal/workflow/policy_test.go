package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (WFX-20, plan §6.9): workflows-policy.json (a machine file an
// admin owns and the user's own one) denies agent steps, write targets,
// schedules or whole workflows by name. A missing file allows all; a
// file that cannot be read or parsed blocks every workflow (fail closed).
func TestPolicyDenies(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	f := &File{Name: "nightly-bugs", Schedule: &Schedule{Preset: "daily", Time: "09:00"}, Steps: []Step{
		{ID: "fix", Type: TypeEach, Each: &EachSpec{Steps: []Step{{ID: "code", Type: TypeAgent}}}},
		{ID: "hook", Type: TypeWrite, Write: &WriteSpec{To: "http"}},
	}}
	plain := &File{Name: "docs", Steps: []Step{{ID: "rep", Type: TypeWrite, Write: &WriteSpec{To: "file"}}}}

	none := LoadPolicy(filepath.Join(dir, "missing.json"))
	if why := none.Blocked(f); why != "" {
		t.Fatalf("no policy file blocked: %s", why)
	}
	p := LoadPolicy(write("a.json", `{"deny": [{"step": "agent"}]}`), write("b.json", `{"deny": [{"write": "http"}, {"schedule": true}]}`))
	why := p.Blocked(f)
	for _, want := range []string{"agent", "http", "a.json", "b.json"} {
		if !strings.Contains(why, want) {
			t.Errorf("blocked %q misses %q", why, want)
		}
	}
	if why := p.BlockedSchedule(f); !strings.Contains(why, "schedule") {
		t.Errorf("schedule: %q", why)
	}
	if why := p.Blocked(plain); why != "" || p.BlockedSchedule(plain) != "" {
		t.Errorf("a plain workflow was blocked: %q", why)
	}
	byName := LoadPolicy(write("c.json", `{"deny": [{"workflow": "nightly-*"}]}`))
	if byName.Blocked(f) == "" || byName.Blocked(plain) != "" {
		t.Error("deny by name")
	}
	for _, bad := range []string{`{"deny": [`, `{"deny": [{}]}`, `{"deny": [{"step": "nope"}]}`} {
		broken := LoadPolicy(write("bad.json", bad))
		if why := broken.Blocked(plain); !strings.Contains(why, "bad.json") {
			t.Errorf("%s: a broken policy must block everything, got %q", bad, why)
		}
	}
}
