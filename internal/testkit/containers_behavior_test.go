package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

func TestContainersStartAndRemoveCard(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	wd, _ := os.Getwd()
	fixtureDir := filepath.Join(wd, "..", "containers", "testdata", "docker27")
	f := servicestest.New(t)
	for _, pair := range [][2]string{{"version.json", "version.json"}, {"ps.json", "ps.json"}, {"images.json", "images.json"}, {"network-ls.json", "network-ls.json"}, {"volume-ls.json", "volume-ls.json"}, {"context-ls.json", "context-ls.json"}} {
		b, err := os.ReadFile(filepath.Join(fixtureDir, pair[1]))
		if err != nil {
			t.Fatal(err)
		}
		f.Set(t, pair[0], string(b))
	}
	ps, err := os.ReadFile(filepath.Join(fixtureDir, "ps.json"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(ps), "\n")
	for i, line := range lines {
		if strings.Contains(line, `"Names":["nginx","proxy"]`) {
			lines[i] = strings.Replace(line, `"State":"exited"`, `"State":"running"`, 1)
			lines[i] = strings.Replace(lines[i], `"Status":"Exited (1) 3 days ago"`, `"Status":"Up 3 days"`, 1)
		}
	}
	f.Set(t, "ps.json", strings.Join(lines, "\n"))
	t.Setenv("PATH", f.PathEnv())
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: map[string]string{"compose.yaml": "services:\n  api:\n    image: ghcr.io/acme/api:1.4\n"}})
	d.Palette("containers-focus")
	if !d.WaitForText("nginx", 5*time.Second) {
		t.Fatalf("container list did not load:\n%s", screen(d))
	}
	d.Key("g")
	for i := 0; i < 20; i++ {
		if strings.Contains(cursorLine(screen(d)), "nginx") {
			break
		}
		d.Key("j")
	}
	d.Key("s")
	if !d.WaitFor(func() bool { return f.Count("start aabbccdd") == 1 }, 5*time.Second) {
		t.Fatalf("start did not run against the full ID: %q\n%s", f.Calls(), screen(d))
	}
	d.Key("D")
	if !d.WaitFor(func() bool {
		frame := screen(d)
		return strings.Contains(frame, "stop aabbccdd") && strings.Contains(frame, "rm aabbccdd") && strings.Contains(frame, "Stop and remove")
	}, 3*time.Second) {
		t.Fatalf("running removal card does not name the exact safe command:\n%s", screen(d))
	}
	if f.Count("rm -f") != 0 {
		t.Fatalf("force remove was used: %q", f.Calls())
	}
	d.Key("right")
	d.Key("enter")
	if !d.WaitFor(func() bool { return f.Count("stop aabbccdd") == 1 && f.Count("rm aabbccdd") == 1 }, 5*time.Second) {
		t.Fatalf("Stop and remove did not run stop before remove: %q\n%s", f.Calls(), screen(d))
	}
	calls := strings.Join(f.Calls(), "\n")
	stopAt, removeAt := strings.Index(calls, "stop aabbccdd"), strings.Index(calls, "rm aabbccdd")
	if stopAt < 0 || removeAt < stopAt || strings.Contains(calls, "rm -f") {
		t.Fatalf("unsafe removal call order: %q", f.Calls())
	}
}

func TestContainersComposeDownCard(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	wd, _ := os.Getwd()
	fixtureDir := filepath.Join(wd, "..", "containers", "testdata", "docker27")
	f := servicestest.New(t)
	for _, name := range []string{"version.json", "images.json", "network-ls.json", "volume-ls.json", "context-ls.json"} {
		b, err := os.ReadFile(filepath.Join(fixtureDir, name))
		if err != nil {
			t.Fatal(err)
		}
		f.Set(t, name, string(b))
	}
	ps, err := os.ReadFile(filepath.Join(fixtureDir, "ps.json"))
	if err != nil {
		t.Fatal(err)
	}
	data := strings.Replace(string(ps), "com.docker.compose.project.working_dir=/work/termocode", "com.docker.compose.project.working_dir=,com.docker.compose.project.config_files=compose.yaml,compose.override.yaml", 1)
	f.Set(t, "ps.json", data)
	t.Setenv("PATH", f.PathEnv())
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: map[string]string{"compose.yaml": "services:\n  api:\n    image: ghcr.io/acme/api:1.4\n"}})
	data = strings.Replace(data, "working_dir=,", "working_dir="+d.Dir+",", 1)
	f.Set(t, "ps.json", data)
	d.Palette("containers-focus")
	if !d.WaitForText("termocode", 5*time.Second) {
		t.Fatalf("project containers did not load:\n%s", screen(d))
	}
	svcGoto(d, "termocode")
	d.Key("D")
	if !d.WaitFor(func() bool {
		frame := screen(d)
		return strings.Contains(frame, "Remove project containers?") && strings.Contains(frame, "compose.override.yaml") && strings.Contains(frame, "Down")
	}, 3*time.Second) {
		t.Fatalf("compose down card did not name its project and files:\n%s", screen(d))
	}
	if strings.Contains(strings.Join(f.Calls(), "\n"), "compose down") {
		t.Fatalf("compose down ran before confirmation: %q", f.Calls())
	}
}

func cursorLine(frame string) string {
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, "▌") {
			return line
		}
	}
	return ""
}
