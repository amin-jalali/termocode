package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// containers_storage: volumes and networks of any host as the GUI does
// them (create with options, remove, browse, back up, restore, clone,
// disk usage). containers_prune: every prune of the GUI. The GUI's cards
// (remove, restore, clone, every prune) ask every time with no "Always
// allow"; create and back up ask with the containers class; browse and
// disk usage read at once.

func TestAIContainersStorageGates(t *testing.T) {
	m, _, call := aiContainersTestModel(t)
	tools := m.aiContainersTools(call)
	st := aiContainersTool(t, tools, "containers_storage")
	want := "volume_create,network_create,volume_remove,network_remove,volume_browse,volume_backup,volume_restore,volume_clone,disk_usage"
	if got := aiParamEnum(st.Params["op"]); strings.Join(got, ",") != want {
		t.Errorf("ops = %v", got)
	}
	for op, w := range map[string][2]any{
		"volume_remove": {true, ""}, "network_remove": {true, ""}, "volume_restore": {true, ""}, "volume_clone": {true, ""},
		"volume_create": {true, ai.ClassContainers}, "network_create": {true, ai.ClassContainers}, "volume_backup": {true, ai.ClassContainers},
		"volume_browse": {false, ""}, "disk_usage": {false, ""},
	} {
		if ask, class := st.Gate(map[string]any{"op": op}); ask != w[0] || class != w[1] {
			t.Errorf("%s gate = %v %q, want %v %q", op, ask, class, w[0], w[1])
		}
	}
	pr := aiContainersTool(t, tools, "containers_prune")
	if got := aiParamEnum(pr.Params["kind"]); strings.Join(got, ",") != "containers,images,images-all,networks,volumes,system,build-cache" {
		t.Errorf("prune kinds = %v", got)
	}
	m.aiContainersAllowSession()
	for _, k := range []string{"containers", "volumes", "system", "build-cache"} {
		if ask, class := pr.Gate(map[string]any{"kind": k}); !ask || class != "" {
			t.Errorf("prune %s gate = %v %q, want a card every time", k, ask, class)
		}
	}
}

func TestAIContainersStorageRuns(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	st := aiContainersTool(t, m.aiContainersTools(call), "containers_storage")
	run := func(a map[string]any) string {
		t.Helper()
		if err := st.Validate(a); err != nil {
			t.Fatalf("%v: %v", a, err)
		}
		out, err := st.Run(context.Background(), a)
		if err != nil {
			t.Fatalf("%v: %v", a, err)
		}
		return out
	}
	run(map[string]any{"op": "volume_create", "name": "cache", "driver": "local"})
	run(map[string]any{"op": "network_create", "name": "back", "subnet": "10.9.0.0/24", "internal": true})
	covAIMustContain(t, "rm card", st.Describe(map[string]any{"op": "network_remove", "name": "termocode_default"}), "Remove network termocode_default", "docker network rm termocode_default")
	run(map[string]any{"op": "network_remove", "name": "termocode_default"})
	f.Set(t, "ps.json", "") // no container uses the volume
	covAIMustContain(t, "volume rm card", st.Describe(map[string]any{"op": "volume_remove", "name": "termocode_db"}), "Remove volume termocode_db", "its data", "cannot be undone")
	run(map[string]any{"op": "volume_remove", "name": "termocode_db"})
	run(map[string]any{"op": "volume_backup", "name": "termocode_db", "dir": "backups", "file": "db.tgz"})
	run(map[string]any{"op": "volume_clone", "name": "termocode_db", "to": "termocode_db_copy"})
	var acts []string
	for _, c := range f.Calls() {
		for _, p := range []string{"volume create", "network create", "network rm", "volume rm", "run "} {
			if strings.HasPrefix(c, p) {
				acts = append(acts, c)
			}
		}
	}
	bk := filepath.Join(m.services.root, "backups")
	want := []string{"volume create --driver local -- cache", "network create --subnet 10.9.0.0/24 --internal", "network rm termocode_default",
		"volume rm termocode_db", "-v termocode_db:/v:ro -v " + bk + ":/backup", "-v termocode_db:/from:ro -v termocode_db_copy:/to"}
	if len(acts) != len(want) {
		t.Fatalf("ran %q", acts)
	}
	for i, w := range want {
		if !strings.Contains(acts[i], w) {
			t.Errorf("call %d %q, want %q", i, acts[i], w)
		}
	}
	for _, bad := range []map[string]any{
		{"op": "network_remove", "name": "bridge"},
		{"op": "network_remove", "name": "nope"},
		{"op": "volume_create", "name": "-x"},
		{"op": "network_create", "name": "n", "subnet": "x"},
		{"op": "volume_backup", "name": "termocode_db", "dir": "../out", "file": "db.tgz"},
		{"op": "volume_clone", "name": "termocode_db", "to": "termocode_db"},
	} {
		if err := st.Validate(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	// A volume a container uses stays (the GUI's "Volume is in use").
	f.Set(t, "ps.json", `{"ID":"aabbccdd","Names":"db","Image":"postgres:16","State":"running","Labels":{}}`+"\n")
	if err := st.Validate(map[string]any{"op": "volume_remove", "name": "termocode_db"}); err == nil || !strings.Contains(err.Error(), "db") {
		t.Errorf("remove of a used volume: %v", err)
	}
}

func TestAIContainersPruneRuns(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	pr := aiContainersTool(t, m.aiContainersTools(call), "containers_prune")
	f.Set(t, "prune.out", "Total reclaimed space: 1.2GB\n")
	covAIMustContain(t, "prune card", pr.Describe(map[string]any{"kind": "volumes"}), "Remove unused volumes", "docker volume prune -f", "data is deleted")
	for _, k := range []string{"containers", "images-all", "system"} {
		out, err := pr.Run(context.Background(), map[string]any{"kind": k})
		if err != nil || !strings.Contains(out, "1.2GB") {
			t.Errorf("prune %s: %q %v", k, out, err)
		}
	}
	if _, err := pr.Run(context.Background(), map[string]any{"kind": "build-cache", "all": true}); err != nil {
		t.Error(err)
	}
	for _, w := range []string{"container prune -f", "image prune -a -f", "system prune -f", "builder prune --force --all"} {
		if f.Count(w) != 1 {
			t.Errorf("no %q in %q", w, f.Calls())
		}
	}
	if err := pr.Validate(map[string]any{"kind": "everything"}); err == nil {
		t.Error("prune everything accepted")
	}
}
