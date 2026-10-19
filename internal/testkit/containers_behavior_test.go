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

func TestContainersDanglingImagePruneCard(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	wd, _ := os.Getwd()
	fixtureDir := filepath.Join(wd, "..", "containers", "testdata", "docker27")
	f := servicestest.New(t)
	for _, name := range []string{"version.json", "ps.json", "images.json", "network-ls.json", "volume-ls.json", "context-ls.json"} {
		b, err := os.ReadFile(filepath.Join(fixtureDir, name))
		if err != nil {
			t.Fatal(err)
		}
		f.Set(t, name, string(b))
	}
	t.Setenv("PATH", f.PathEnv())
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: map[string]string{"compose.yaml": "services:\n  api:\n    image: sample:1\n"}})
	d.Palette("containers-focus")
	if !d.WaitForText("IMAGES", 5*time.Second) {
		t.Fatalf("image section did not load:\n%s", screen(d))
	}
	svcGoto(d, "IMAGES")
	d.Key("z")
	if !d.WaitForText("Remove dangling images?", 3*time.Second) {
		t.Fatalf("prune card omitted the exact dangling-only command:\n%s", screen(d))
	}
	commandText := strings.Join(strings.Fields(screen(d)), " ")
	if !strings.Contains(commandText, "image prune -f") {
		t.Fatalf("prune card does not name the exact command:\n%s", screen(d))
	}
	if strings.Contains(commandText, "image prune -a") || strings.Contains(commandText, "prune -af") {
		t.Fatalf("image prune card includes all tagged images:\n%s", screen(d))
	}
	if f.Count("image prune") != 0 {
		t.Fatalf("prune ran before confirmation: %q", f.Calls())
	}
}

func TestContainersNetworkExpandInspectsOnDemand(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	wd, _ := os.Getwd()
	fixtureDir := filepath.Join(wd, "..", "containers", "testdata", "docker27")
	f := servicestest.New(t)
	for _, name := range []string{"version.json", "ps.json", "images.json", "network-ls.json", "volume-ls.json", "context-ls.json", "inspect.json"} {
		b, err := os.ReadFile(filepath.Join(fixtureDir, name))
		if err != nil {
			t.Fatal(err)
		}
		f.Set(t, name, string(b))
	}
	t.Setenv("PATH", f.PathEnv())
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: map[string]string{"compose.yaml": "services:\n  api:\n    image: sample:1\n"}})
	d.Palette("containers-focus")
	if !d.WaitForText("NETWORKS", 5*time.Second) {
		t.Fatalf("network section did not load:\n%s", screen(d))
	}
	svcGoto(d, "NETWORKS")
	d.Key(" ")
	if !d.WaitForText("NETWORKS ─", 5*time.Second) {
		t.Fatalf("network list did not open:\n%s", screen(d))
	}
	d.Key("j")
	before := f.Count("inspect termocode_default")
	d.Key(" ")
	if !d.WaitFor(func() bool { return f.Count("inspect termocode_default") == before+1 }, 5*time.Second) {
		t.Fatalf("expanding network did not inspect it on demand: %q\n%s", f.Calls(), screen(d))
	}
}

func containersRunFixture(t *testing.T) *Driver {
	t.Helper()
	wd, _ := os.Getwd()
	fixtureDir := filepath.Join(wd, "..", "containers", "testdata", "docker27")
	f := servicestest.New(t)
	for _, name := range []string{"version.json", "ps.json", "images.json", "network-ls.json", "volume-ls.json", "context-ls.json"} {
		b, err := os.ReadFile(filepath.Join(fixtureDir, name))
		if err != nil {
			t.Fatal(err)
		}
		f.Set(t, name, string(b))
	}
	t.Setenv("PATH", f.PathEnv())
	d := New(t, Options{W: 140, H: 36, Nvim: true, Files: map[string]string{"compose.yaml": "services:\n  api:\n    image: sample:1\n"}})
	d.Palette("containers-focus")
	if !d.WaitForText("IMAGES", 5*time.Second) {
		t.Fatalf("image section did not load:\n%s", screen(d))
	}
	svcGoto(d, "IMAGES")
	d.Key(" ")
	d.Key("j", "j")
	d.Key("enter")
	if !d.WaitForText("Container name (optional)", 3*time.Second) {
		t.Fatalf("Run image did not open:\n%s", screen(d))
	}
	return d
}

func TestContainersRunImageOptionalStepsFinishOnEmptyEnter(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	d := containersRunFixture(t)
	d.Key("enter") // no name
	if !d.WaitForText("Port to publish", 3*time.Second) {
		t.Fatalf("empty name did not move on:\n%s", screen(d))
	}
	d.Type("8080:80")
	d.Key("enter")
	if !d.WaitForText("Ports so far", 3*time.Second) {
		t.Fatalf("port not added:\n%s", screen(d))
	}
	d.Key("enter") // finish ports
	if !d.WaitForText("Environment variable", 3*time.Second) {
		t.Fatalf("empty port did not finish ports:\n%s", screen(d))
	}
	d.Type("A=1")
	d.Key("enter")
	d.Type("B=2")
	d.Key("enter")
	if !d.WaitForText("Environment so far", 3*time.Second) {
		t.Fatalf("env not added:\n%s", screen(d))
	}
	d.Key("enter") // finish env
	if !d.WaitForText("Run image?", 3*time.Second) {
		t.Fatalf("no run card:\n%s", screen(d))
	}
	text := strings.Join(strings.Fields(screen(d)), " ")
	for _, want := range []string{"run", "-p 8080:80", "-e A=1", "-e B=2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("run card misses %q:\n%s", want, screen(d))
		}
	}
}

func TestContainersRunImageEscCancels(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	d := containersRunFixture(t)
	d.Key("enter")
	d.Key("esc")
	if !d.WaitForText("Run cancelled", 3*time.Second) {
		t.Fatalf("no cancel toast:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "Port to publish") {
		t.Fatalf("prompt still open:\n%s", screen(d))
	}
}
