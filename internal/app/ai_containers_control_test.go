package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// containers_control does every container action of the GUI's row and
// menu: start, stop, restart, pause, unpause, kill, remove, rename,
// restart policy, CPU and memory limits, network connect / disconnect,
// commit and export. It asks where the GUI asks: the GUI's cards (stop
// on the page, kill, remove, policy, limits, networks, replacing a file)
// ask every time with no "Always allow"; the rest asks with the
// containers class (the page's in-page confirm).

func aiControlTool(t *testing.T) (*Model, *servicestest.Fake, ai.Tool, func() []string) {
	t.Helper()
	m, f, call := aiContainersTestModel(t)
	tl := aiContainersTool(t, m.aiContainersTools(call), "containers_control")
	acts := func() []string {
		var out []string
		for _, c := range f.Calls() {
			if !strings.HasPrefix(c, "ps ") && !strings.HasPrefix(c, "context ") && !strings.HasPrefix(c, "network ls") && !strings.HasPrefix(c, "version") {
				out = append(out, c)
			}
		}
		return out
	}
	return m, f, tl, acts
}

func TestAIContainersControlGates(t *testing.T) {
	m, _, tl, _ := aiControlTool(t)
	if got := aiParamEnum(tl.Params["action"]); strings.Join(got, ",") !=
		"start,stop,restart,pause,unpause,kill,remove,rename,restart_policy,resources,network_connect,network_disconnect,commit,export" {
		t.Errorf("actions = %v", got)
	}
	card := []string{"stop", "kill", "remove", "restart_policy", "resources", "network_connect", "network_disconnect"}
	session := []string{"start", "restart", "pause", "unpause", "rename", "commit", "export"}
	for _, a := range card {
		if ask, class := tl.Gate(map[string]any{"action": a}); !ask || class != "" {
			t.Errorf("%s gate = %v %q, want a card every time", a, ask, class)
		}
	}
	for _, a := range session {
		if ask, class := tl.Gate(map[string]any{"action": a}); !ask || class != ai.ClassContainers {
			t.Errorf("%s gate = %v %q, want the containers class", a, ask, class)
		}
	}
	// "Always allow containers" never skips a card action.
	m.aiContainersAllowSession()
	if ask, class := tl.Gate(map[string]any{"action": "stop"}); !ask || class != "" {
		t.Errorf("stop after Always = %v %q", ask, class)
	}
	if ask, _ := tl.Gate(map[string]any{"action": "start"}); ask {
		t.Error("start still asks after Always allow containers")
	}
	// Export over an existing file asks every time (the GUI's Replace card).
	if err := os.WriteFile(filepath.Join(m.services.root, "api.tar"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ask, class := tl.Gate(map[string]any{"action": "export", "target": "termocode-api-1", "file": "api.tar"}); !ask || class != "" {
		t.Errorf("export over a file = %v %q", ask, class)
	}
}

func TestAIContainersControlKillRemove(t *testing.T) {
	_, _, tl, acts := aiControlTool(t)
	kill := map[string]any{"action": "kill", "target": "termocode-api-1"}
	covAIMustContain(t, "kill card", tl.Describe(kill), "Kill container", "docker kill --signal KILL 9f3c2e1a1234", "unsaved work in it is lost")
	if _, err := tl.Run(context.Background(), kill); err != nil {
		t.Fatal(err)
	}
	if _, err := tl.Run(context.Background(), map[string]any{"action": "kill", "target": "termocode-api-1", "signal": "TERM"}); err != nil {
		t.Fatal(err)
	}
	if err := tl.Validate(map[string]any{"action": "kill", "target": "nginx"}); err == nil {
		t.Error("kill of a stopped container accepted")
	}
	if err := tl.Validate(map[string]any{"action": "kill", "target": "termocode-api-1", "signal": "-9; id"}); err == nil {
		t.Error("bad signal accepted")
	}
	if err := tl.Validate(map[string]any{"action": "remove", "target": "termocode-api-1"}); err == nil || !strings.Contains(err.Error(), "force") {
		t.Errorf("remove running without force: %v", err)
	}
	rm := map[string]any{"action": "remove", "target": "nginx"}
	covAIMustContain(t, "remove card", tl.Describe(rm), "Remove container nginx", "docker rm aabbccdd", "cannot be undone")
	if _, err := tl.Run(context.Background(), rm); err != nil {
		t.Fatal(err)
	}
	if _, err := tl.Run(context.Background(), map[string]any{"action": "remove", "target": "termocode-api-1", "force": true}); err != nil {
		t.Fatal(err)
	}
	want := "kill --signal KILL 9f3c2e1a1234|kill --signal TERM 9f3c2e1a1234|rm aabbccdd|stop 9f3c2e1a1234|rm 9f3c2e1a1234"
	if got := strings.Join(acts(), "|"); got != want {
		t.Errorf("ran %q\nwant %q", got, want)
	}
}

func TestAIContainersControlWrites(t *testing.T) {
	m, _, tl, acts := aiControlTool(t)
	run := func(a map[string]any) {
		t.Helper()
		if err := tl.Validate(a); err != nil {
			t.Fatalf("%v: %v", a, err)
		}
		if _, err := tl.Run(context.Background(), a); err != nil {
			t.Fatalf("%v: %v", a, err)
		}
	}
	run(map[string]any{"action": "rename", "target": "nginx", "name": "edge"})
	run(map[string]any{"action": "restart_policy", "target": "nginx", "policy": "unless-stopped"})
	run(map[string]any{"action": "resources", "target": "termocode-api-1", "cpus": "1.5", "memory": "512m"})
	run(map[string]any{"action": "network_connect", "target": "nginx", "network": "termocode_default"})
	run(map[string]any{"action": "network_disconnect", "target": "termocode-api-1", "network": "termocode_default"})
	run(map[string]any{"action": "commit", "target": "termocode-api-1", "image": "api:snap", "message": "before upgrade"})
	run(map[string]any{"action": "export", "target": "nginx", "file": "out/nginx.tar"})
	exp := filepath.Join(m.services.root, "out", "nginx.tar")
	want := "rename aabbccdd edge|update --restart unless-stopped aabbccdd|update --cpus 1.5 --memory 512m 9f3c2e1a1234|" +
		"network connect termocode_default aabbccdd|network disconnect termocode_default 9f3c2e1a1234|" +
		"commit -m before upgrade 9f3c2e1a1234 api:snap|export -o " + exp + " aabbccdd"
	if got := strings.Join(acts(), "|"); got != want {
		t.Errorf("ran %q\nwant %q", got, want)
	}
	for _, bad := range []map[string]any{
		{"action": "rename", "target": "nginx", "name": "nginx"},
		{"action": "rename", "target": "nginx", "name": "a b"},
		{"action": "rename", "target": "nginx", "name": "termocode-api-1"},
		{"action": "restart_policy", "target": "nginx", "policy": "sometimes"},
		{"action": "resources", "target": "nginx"},
		{"action": "resources", "target": "nginx", "memory": "lots"},
		{"action": "network_connect", "target": "termocode-api-1", "network": "termocode_default"},
		{"action": "network_connect", "target": "nginx", "network": "nope"},
		{"action": "network_disconnect", "target": "nginx", "network": "termocode_default"},
		{"action": "commit", "target": "nginx", "image": "-x"},
		{"action": "export", "target": "nginx", "file": "../out.tar"},
		{"action": "export", "target": "nginx", "file": "/etc/out.tar"},
		{"action": "export", "target": "nginx"},
	} {
		if err := tl.Validate(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

// images_pull runs without a question, as the GUI pulls; containers_inspect
// reads a container's changed files and its processes.
func TestAIContainersPullAtOnceAndInspectChanges(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	tools := m.aiContainersTools(call)
	pull := aiContainersTool(t, tools, "images_pull")
	if pull.Gate == nil {
		t.Fatal("images_pull has no gate")
	}
	if ask, _ := pull.Gate(map[string]any{"ref": "nginx:1.27"}); ask {
		t.Error("images_pull asks; the GUI pulls without a question")
	}
	ins := aiContainersTool(t, tools, "containers_inspect")
	if got := aiParamEnum(ins.Params["kind"]); strings.Join(got, ",") != "container,image,volume,network,changes,processes" {
		t.Errorf("inspect kinds = %v", got)
	}
	for _, k := range []string{"changes", "processes"} {
		if _, err := ins.Run(context.Background(), map[string]any{"target": "termocode-api-1", "kind": k}); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
	if f.Count("diff 9f3c2e1a1234") != 1 || f.Count("top 9f3c2e1a1234") != 1 {
		t.Errorf("calls %q", f.Calls())
	}
}
