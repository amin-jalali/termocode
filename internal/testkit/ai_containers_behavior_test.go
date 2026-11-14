package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// aiContainersFake is the fake docker with the docker27 fixtures; the
// api container's command carries the .env password (it must never
// reach the model), and inspect shows it in Config.Env.
func aiContainersFake(t *testing.T) *servicestest.Fake {
	t.Helper()
	wd, _ := os.Getwd()
	dir := filepath.Join(wd, "..", "containers", "testdata", "docker27")
	f := servicestest.New(t)
	for _, name := range []string{"version.json", "ps.json", "images.json", "network-ls.json", "volume-ls.json", "context-ls.json"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if name == "ps.json" {
			s = strings.Replace(s, `"Command":"\"./api\""`, `"Command":"\"./api --seed hunter2hunter2\""`, 1)
		}
		f.Set(t, name, s)
	}
	f.Set(t, "inspect.json", `[{"Id":"9f3c2e1a1234","Name":"/termocode-api-1","Config":{"Image":"ghcr.io/acme/api:1.4",`+
		`"Env":["POSTGRES_PASSWORD=hunter2hunter2","PORT=8080"],"Labels":{"com.docker.compose.project":"termocode"}}}]`)
	t.Setenv("PATH", f.PathEnv())
	t.Setenv("DOCKER_CONTEXT", "")
	return f
}

// CP-9, the plan's flow: "why is 8081 busy?" The model lists the host
// containers (no card), finds who publishes 8081, inspects it (env values
// masked), then tries to stop it: a bad id is refused with no card; the
// first card is denied and nothing runs; stop asks every time (no Always
// allow), so the second card gets Allow and runs exactly `stop <id>`; the
// restart's card offers "Always allow containers", which runs it. The .env
// password never reaches the model or the screen, and the "always" choice
// is not saved in config.json.
func TestAIContainersPortBusyFlow(t *testing.T) {
	f := aiContainersFake(t)
	s, srv := fakeToolModel(t,
		toolCall{"containers_list", map[string]any{}},
		toolCall{"containers_inspect", map[string]any{"target": "termocode-api-1"}},
		toolCall{"containers_control", map[string]any{"action": "stop", "target": "-f"}},
		toolCall{"containers_control", map[string]any{"action": "stop", "target": "termocode-api-1"}},
		toolCall{"containers_control", map[string]any{"action": "stop", "target": "termocode-api-1"}},
		toolCall{"containers_control", map[string]any{"action": "restart", "target": "termocode-api-1"}},
	)
	files := textFiles()
	files[".env"] = "POSTGRES_PASSWORD=hunter2hunter2\n"
	d := aiDriver(t, srv.URL, files, false)
	open(d, "a.txt")
	askAI(d, "why is 8081 busy?")

	if !d.WaitForText("AI wants to", 20*time.Second) {
		t.Fatalf("no confirm card for containers_control:\n%s", screen(d))
	}
	sc := screen(d)
	for _, want := range []string{"containers_control", "Stop container api-1", "stop 9f3c2e1a1234", "context default",
		"Allow", "Deny"} {
		if !strings.Contains(sc, want) {
			t.Errorf("confirm card has no %q:\n%s", want, sc)
		}
	}
	for _, never := range []string{"Always allow containers", "Always in project"} {
		if strings.Contains(sc, never) {
			t.Errorf("stop asks every time; its card must not offer %q:\n%s", never, sc)
		}
	}
	clickTextOn(d, "Deny")
	if !d.WaitFor(func() bool { return len(s.toolResults()) >= 4 }, 10*time.Second) {
		t.Fatalf("the denied call did not answer:\n%s", screen(d))
	}
	if n := f.Count("stop "); n != 0 {
		t.Fatalf("Deny ran a stop: %q", f.Calls())
	}
	if !d.WaitFor(func() bool { return strings.Contains(screen(d), "AI wants to") && len(s.toolResults()) == 4 }, 10*time.Second) {
		t.Fatalf("no second card:\n%s", screen(d))
	}
	clickAIAllow(d)
	if !d.WaitForText("Always allow containers", 10*time.Second) {
		t.Fatalf("no restart card with Always allow containers:\n%s", screen(d))
	}
	clickTextOn(d, "Always allow containers")
	if !d.WaitForText("fake-done", 20*time.Second) {
		t.Fatalf("no final answer (a dialog may be waiting):\n%s", screen(d))
	}
	var acts []string
	for _, c := range f.Calls() {
		for _, verb := range []string{"start ", "stop ", "restart ", "pause ", "unpause ", "rm ", "pull ", "kill "} {
			if strings.HasPrefix(c, verb) {
				acts = append(acts, c)
			}
		}
	}
	if strings.Join(acts, "|") != "stop 9f3c2e1a1234|restart 9f3c2e1a1234" {
		t.Errorf("actions = %q, want exactly stop then restart of 9f3c2e1a1234", acts)
	}
	r := s.toolResults()
	if len(r) != 6 {
		t.Fatalf("%d results: %q", len(r), r)
	}
	all := strings.Join(r, "\n=====\n")
	if strings.Contains(all, "hunter2hunter2") || strings.Contains(screen(d), "hunter2hunter2") {
		t.Errorf("the password reached the model or the screen:\n%s", all)
	}
	checks := []struct {
		i    int
		want []string
	}{
		{0, []string{"Engine: docker · context default", "port 8081 → termocode-api-1", "project termocode", "nginx", "[REDACTED]"}},
		{1, []string{"POSTGRES_PASSWORD=[REDACTED]", "PORT=[REDACTED]", "Inspect container termocode-api-1"}},
		{2, []string{"error:", "cannot start with"}},
		{3, []string{"declined"}},
		{4, []string{"ran `docker stop 9f3c2e1a1234`", "context default"}},
		{5, []string{"ran `docker restart 9f3c2e1a1234`"}},
	}
	for _, c := range checks {
		for _, w := range c.want {
			if !strings.Contains(r[c.i], w) {
				t.Errorf("result %d has no %q:\n%s", c.i, w, r[c.i])
			}
		}
	}
	cfg, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "config.json"))
	if strings.Contains(string(cfg), `"containers"`) {
		t.Errorf("\"Always allow containers\" was saved in config.json: %s", cfg)
	}
}

// CP-9: images_pull from the fake model. A flag-like ref is refused with
// no card; a real ref opens, with no card (as the GUI pulls), the visible
// "pull: nginx:1.27" tab that runs exactly `pull nginx:1.27`.
func TestAIContainersImagesPullFlow(t *testing.T) {
	f := aiContainersFake(t)
	s, srv := fakeToolModel(t,
		toolCall{"images_pull", map[string]any{"ref": "--all-tags"}},
		toolCall{"images_pull", map[string]any{"ref": "nginx:1.27"}},
	)
	d := aiDriver(t, srv.URL, nil, false)
	open(d, "a.txt")
	askAI(d, "pull nginx 1.27 please")
	// images_pull runs at once, as the Containers view pulls without a
	// question (in a visible terminal tab).
	if !d.WaitForText("fake-done", 20*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	if !d.WaitFor(func() bool { return f.Count("pull nginx:1.27") == 1 }, 10*time.Second) {
		t.Fatalf("the pull tab did not run `pull nginx:1.27`: %q\n%s", f.Calls(), screen(d))
	}
	if f.Count("--all-tags") != 0 {
		t.Errorf("the refused ref ran: %q", f.Calls())
	}
	if !d.WaitForText("pull: nginx:1.27", 5*time.Second) {
		t.Errorf("no pull tab on screen:\n%s", screen(d))
	}
	r := s.toolResults()
	if len(r) != 2 || !strings.Contains(r[0], "error:") || !strings.Contains(r[1], "pull: nginx:1.27") {
		t.Errorf("results: %q", r)
	}
}
