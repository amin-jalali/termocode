package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/services"
)

// compose_project: a compose project of any host as the GUI's project
// row and its menu: up, stop, restart, recreate, down, down with volumes,
// pull, scale, logs, and Deploy Stack from Git. Stop, down and down with
// volumes are the GUI's cards (every time, no "Always allow"); up,
// restart, recreate, pull and scale ask with the containers class; logs
// read at once; deploy_git opens the GUI's own Deploy card.

func TestAIComposeProject(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	dir := t.TempDir()
	f.Set(t, "ps.json", `{"ID":"9f3c2e1a1234","Names":"shop-api-1","Image":"api","State":"running","Labels":"com.docker.compose.project=shop,com.docker.compose.project.working_dir=`+
		dir+`,com.docker.compose.project.config_files=`+filepath.Join(dir, "compose.yml")+`"}`+"\n")
	f.Set(t, "logs-api.txt", "api | listening on :8080\n")
	var tabs []aiCtrTab
	old := aiCtrTerminal
	aiCtrTerminal = func(_ *Model, c services.Command, key string) error {
		tabs = append(tabs, aiCtrTab{c, key})
		return nil
	}
	t.Cleanup(func() { aiCtrTerminal = old })
	tl := aiContainersTool(t, m.aiContainersTools(call), "compose_project")
	if got := aiParamEnum(tl.Params["op"]); strings.Join(got, ",") != "up,stop,restart,recreate,down,down_volumes,pull,scale,logs,deploy_git" {
		t.Errorf("ops = %v", got)
	}
	for op, w := range map[string][2]any{
		"stop": {true, ""}, "down": {true, ""}, "down_volumes": {true, ""},
		"up": {true, ai.ClassContainers}, "restart": {true, ai.ClassContainers}, "recreate": {true, ai.ClassContainers},
		"pull": {true, ai.ClassContainers}, "scale": {true, ai.ClassContainers},
		"logs": {false, ""}, "deploy_git": {false, ""},
	} {
		if ask, class := tl.Gate(map[string]any{"op": op}); ask != w[0] || class != w[1] {
			t.Errorf("%s gate = %v %q, want %v %q", op, ask, class, w[0], w[1])
		}
	}
	covAIMustContain(t, "down card", tl.Describe(map[string]any{"op": "down", "project": "shop"}), "Remove project containers shop", "compose -p shop", "down", "Volumes stay")
	covAIMustContain(t, "down volumes card", tl.Describe(map[string]any{"op": "down_volumes", "project": "shop"}), "down --volumes", "data in the volumes is lost")
	for _, op := range []string{"stop", "restart", "recreate", "down", "down_volumes", "pull"} {
		if _, err := tl.Run(context.Background(), map[string]any{"op": op, "project": "shop"}); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
	}
	if _, err := tl.Run(context.Background(), map[string]any{"op": "scale", "project": "shop", "service": "api", "replicas": 3}); err != nil {
		t.Fatal(err)
	}
	want := []string{"stop", "restart", "up -d --force-recreate", "down", "down --volumes", "pull", "up -d --scale api=3"}
	if len(tabs) != len(want) {
		t.Fatalf("tabs = %+v", tabs)
	}
	for i, w := range want {
		argv := strings.Join(tabs[i].cmd.Argv, " ")
		if !strings.Contains(argv, "compose -p shop --project-directory "+dir) || !strings.HasSuffix(argv, " "+w) {
			t.Errorf("tab %d = %q, want … %s", i, argv, w)
		}
	}
	out, err := tl.Run(context.Background(), map[string]any{"op": "logs", "project": "shop", "services": []any{"api"}})
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "logs", out, "listening on :8080")
	for _, bad := range []map[string]any{
		{"op": "scale", "project": "shop", "service": "api", "replicas": 101},
		{"op": "scale", "project": "shop", "service": "-x", "replicas": 1},
		{"op": "stop", "project": "nope"},
		{"op": "deploy_git", "url": "ftp://x"},
	} {
		if err := tl.Validate(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}
