package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webPathFields lists the path fields of an args struct.
func webPathFields(v any) []reflect.Value {
	rv := reflect.ValueOf(v).Elem()
	var out []reflect.Value
	for i := 0; i < rv.NumField(); i++ {
		switch rv.Type().Field(i).Name {
		case "Path", "Paths":
			out = append(out, rv.Field(i))
		}
	}
	return out
}

// Spec (T40): every op that takes a path resolves it inside the
// workspace with symlinks followed: a path that passes the wire rules
// (relative, no "..") but leads outside through a symlink is refused by
// every op, and nothing outside is read, listed or opened.
func TestEveryPathOpUsesResolve(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	r := newWebFilesRig(t)
	r.m.output = newOutputStore()
	r.withNvim()
	outside := filepath.Join(filepath.Dir(r.root), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside-body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(r.root, "out")); err != nil {
		t.Fatal(err)
	}
	r.write("a.txt", "one\n")
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "t@e.x"}, {"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"}, {"add", "a.txt"}, {"commit", "-q", "-m", "first"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = r.root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	webTestGitLoad(r.m)

	tried := 0
	for _, p := range webproto.Ops() {
		if p.Role == webproto.RoleCtl {
			continue
		}
		probe := p.New()
		if len(webPathFields(probe)) == 0 {
			continue
		}
		if r.h.ops[p.Op] == nil && webAskOps[p.Op] == nil {
			continue // no handler yet: the hub answers not_found
		}
		for _, bad := range []string{"out/secret.txt", "out"} {
			args, err := webproto.DecodeArgs(p.Op, webGoldenArgs(t, p.Op))
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range webPathFields(args) {
				if f.Kind() == reflect.Slice {
					f.Set(reflect.ValueOf([]string{bad}))
				} else {
					f.SetString(bad)
				}
			}
			raw, _ := json.Marshal(args)
			if _, err := webproto.DecodeArgs(p.Op, raw); err != nil {
				t.Fatalf("%s %s: the wire rules refused it (%v); the test needs a path that passes them", p.Op, bad, err)
			}
			tried++
			ans := r.call(p.Op, args)
			if p.Op == webproto.OpTreeUnlist && ans["t"] == "res" {
				continue // stops watching a folder: reads nothing
			}
			if ans["t"] != "err" {
				t.Errorf("%s %q: %v", p.Op, bad, ans)
			}
			if b, _ := json.Marshal(ans); strings.Contains(string(b), "outside-body") || strings.Contains(string(b), "secret.txt") {
				t.Errorf("%s %q shows what is outside: %s", p.Op, bad, b)
			}
		}
	}
	if tried < 10 {
		t.Fatalf("only %d path ops tried", tried)
	}
	if r.m.confirmOpen {
		t.Error("a path outside the workspace reached a laptop card")
	}
}
