package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// images_manage: every image action of the Containers view on any host:
// push, tag, run, remove, build, history, used_by, save, load, import,
// scan, sbom, Docker Hub search, remote tags, update check, builds, build
// detail and the Dockerfile check. Push, run, remove, build and load are
// the GUI's cards: they ask every time with no "Always allow"; tag, save
// and import ask with the containers class (a tag that moves from another
// image and a save over a file are cards); the reads run at once.

func aiImagesTool(t *testing.T) (*Model, *servicestest.Fake, ai.Tool, *[]aiCtrTab, func() []string) {
	t.Helper()
	m, f, call := aiContainersTestModel(t)
	var tabs []aiCtrTab
	old := aiCtrTerminal
	aiCtrTerminal = func(_ *Model, c services.Command, key string) error { tabs = append(tabs, aiCtrTab{c, key}); return nil }
	t.Cleanup(func() { aiCtrTerminal = old })
	acts := func() []string {
		var out []string
		for _, c := range f.Calls() {
			if !strings.HasPrefix(c, "ps ") && !strings.HasPrefix(c, "context ") && !strings.HasPrefix(c, "images ") && !strings.HasPrefix(c, "version") {
				out = append(out, c)
			}
		}
		return out
	}
	return m, f, aiContainersTool(t, m.aiContainersTools(call), "images_manage"), &tabs, acts
}

func TestAIImagesManageGates(t *testing.T) {
	m, _, tl, _, _ := aiImagesTool(t)
	want := "push,tag,run,remove,build,history,used_by,save,load,import,scan,sbom,search,tags,check_updates,builds,build_detail,build_check"
	if got := aiParamEnum(tl.Params["op"]); strings.Join(got, ",") != want {
		t.Errorf("ops = %v", got)
	}
	for _, op := range []string{"push", "run", "remove", "build", "load"} {
		if ask, class := tl.Gate(map[string]any{"op": op}); !ask || class != "" {
			t.Errorf("%s gate = %v %q, want a card every time", op, ask, class)
		}
	}
	for _, op := range []string{"tag", "save", "import"} {
		if ask, class := tl.Gate(map[string]any{"op": op}); !ask || class != ai.ClassContainers {
			t.Errorf("%s gate = %v %q, want the containers class", op, ask, class)
		}
	}
	for _, op := range []string{"history", "used_by", "scan", "sbom", "search", "tags", "check_updates", "builds", "build_detail", "build_check"} {
		if ask, _ := tl.Gate(map[string]any{"op": op}); ask {
			t.Errorf("%s asks; the GUI reads it at once", op)
		}
	}
	// Moving a tag from another image and saving over a file are cards.
	if ask, class := tl.Gate(map[string]any{"op": "tag", "ref": "postgres:16", "tag": "nginx:1.27"}); !ask || class != "" {
		t.Errorf("moving a tag = %v %q", ask, class)
	}
	if err := os.WriteFile(filepath.Join(m.services.root, "pg.tar"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ask, class := tl.Gate(map[string]any{"op": "save", "ref": "postgres:16", "file": "pg.tar"}); !ask || class != "" {
		t.Errorf("save over a file = %v %q", ask, class)
	}
}

func TestAIImagesManageWrites(t *testing.T) {
	m, f, tl, tabs, acts := aiImagesTool(t)
	f.Set(t, "images.json", `{"ID":"sha256:111","Repository":"postgres","Tag":"16","Size":"421MB"}`+"\n"+
		`{"ID":"sha256:333","Repository":"ghcr.io/acme/api","Tag":"1.4","Size":"90MB"}`+"\n"+
		`{"ID":"sha256:444","Repository":"nginx","Tag":"1.27","Size":"50MB"}`+"\n")
	if err := os.WriteFile(filepath.Join(m.services.root, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(a map[string]any) string {
		t.Helper()
		if err := tl.Validate(a); err != nil {
			t.Fatalf("%v: %v", a, err)
		}
		out, err := tl.Run(context.Background(), a)
		if err != nil {
			t.Fatalf("%v: %v", a, err)
		}
		return out
	}
	covAIMustContain(t, "push card", tl.Describe(map[string]any{"op": "push", "ref": "postgres:16"}), "Push image postgres:16", "docker push postgres:16", "leaves this machine")
	run(map[string]any{"op": "push", "ref": "postgres:16"})
	run(map[string]any{"op": "run", "ref": "postgres:16", "name": "pg", "ports": []any{"5433:5432"}, "detach": true})
	run(map[string]any{"op": "build", "file": "Dockerfile", "tag": "api:dev"})
	run(map[string]any{"op": "tag", "ref": "postgres:16", "tag": "postgres:stable"})
	run(map[string]any{"op": "remove", "ref": "postgres:16"})
	run(map[string]any{"op": "save", "ref": "postgres:16", "file": "out/pg.tar"})
	if len(*tabs) != 3 {
		t.Fatalf("tabs = %+v", *tabs)
	}
	for i, w := range []string{"push postgres:16", "run --name pg -p 5433:5432 -d", "build -t api:dev"} {
		if got := strings.Join((*tabs)[i].cmd.Argv, " "); !strings.Contains(got, w) {
			t.Errorf("tab %d argv %q, want %q", i, got, w)
		}
	}
	want := "tag postgres:16 postgres:stable|rmi postgres:16|save -o " + filepath.Join(m.services.root, "out", "pg.tar") + " postgres:16"
	if got := strings.Join(acts(), "|"); got != want {
		t.Errorf("ran %q\nwant %q", got, want)
	}
	for _, bad := range []map[string]any{
		{"op": "push", "ref": "nope:1"},                       // not on the host
		{"op": "push", "ref": "sha256:111"},                   // not a tag
		{"op": "remove", "ref": "ghcr.io/acme/api:1.4"},       // a container uses it
		{"op": "run", "ref": "postgres:16", "ports": []any{"-p"}},
		{"op": "tag", "ref": "postgres:16", "tag": "-x"},
		{"op": "build", "file": "../Dockerfile"},
		{"op": "save", "ref": "postgres:16", "file": "/tmp/pg.tar"},
		{"op": "load", "file": "missing.tar"},
	} {
		if err := tl.Validate(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestAIImagesManageReads(t *testing.T) {
	_, f, tl, _, _ := aiImagesTool(t)
	f.Set(t, "history.json", `{"ID":"sha256:aaa","CreatedBy":"ENV POSTGRES_PASSWORD=hunter2hunter2","Size":"0B"}`+"\n"+
		`{"ID":"sha256:bbb","CreatedBy":"RUN apt-get install -y libpq","Size":"20MB"}`+"\n")
	out, err := tl.Run(context.Background(), map[string]any{"op": "history", "ref": "postgres:16"})
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "history", out, "apt-get install", "POSTGRES_PASSWORD")
	if strings.Contains(out, "hunter2hunter2") {
		t.Errorf("history leaked an ENV value:\n%s", out)
	}
	out, err = tl.Run(context.Background(), map[string]any{"op": "used_by", "ref": "ghcr.io/acme/api:1.4"})
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "used_by", out, "termocode-api-1")
	f.Set(t, "search.json", `{"Name":"redis","Description":"An in-memory store","StarCount":"12000","IsOfficial":"[OK]"}`+"\n")
	out, err = tl.Run(context.Background(), map[string]any{"op": "search", "term": "redis"})
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "search", out, "redis", "official")
	if err := tl.Validate(map[string]any{"op": "tags", "ref": "-x"}); err == nil {
		t.Error("tags of -x accepted")
	}
}
