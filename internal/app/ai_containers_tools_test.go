package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// CP-9: the AI tools of the Containers view (ai_containers_tools.go).

var aiContainersReadNames = []string{"containers_list", "containers_inspect", "containers_logs", "containers_stats",
	"images_list", "volumes_list", "networks_list", "contexts_list", "registries_list"}
var aiContainersWriteNames = []string{"containers_control", "images_pull", "compose_up"}

// aiContainersTestModel is a model on the fake docker (docker27 fixtures)
// with a synchronous IDE caller.
func aiContainersTestModel(t *testing.T) (*Model, *servicestest.Fake, aiIDECaller) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	f := hostFake(t)
	st := newServicesState()
	st.root = t.TempDir()
	m := &Model{services: st}
	call := func(ctx context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(m)
		return out, err
	}
	return m, f, call
}

func aiContainersTool(t *testing.T, tools []ai.Tool, name string) ai.Tool {
	t.Helper()
	tl, ok := ai.FindTool(tools, name)
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	return tl
}

// Schema: exactly these tools; read tools never ask; write tools ask
// with class containers; containers_control offers only the five verbs.
func TestAIContainersToolsSchema(t *testing.T) {
	m := &Model{services: newServicesState()}
	tools := m.aiContainersTools(nil)
	var names []string
	for _, tl := range tools {
		names = append(names, tl.Name)
		spec := tl.Spec()
		b, err := json.Marshal(spec.Schema)
		if err != nil || !strings.Contains(string(b), `"type":"object"`) {
			t.Errorf("%s: schema %s %v", tl.Name, b, err)
		}
		for _, r := range tl.Required {
			if _, ok := tl.Params[r]; !ok {
				t.Errorf("%s: required %q is not a param", tl.Name, r)
			}
		}
		if tl.Description == "" || tl.Describe == nil {
			t.Errorf("%s: no description / Describe", tl.Name)
		}
	}
	want := append(append([]string(nil), aiContainersReadNames...), aiContainersWriteNames...)
	sort.Strings(names)
	sort.Strings(want)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("tools = %v, want %v", names, want)
	}
	for _, n := range aiContainersReadNames {
		tl := aiContainersTool(t, tools, n)
		if tl.Mutating || tl.Gate != nil || tl.Class != "" {
			t.Errorf("%s must run without a card", n)
		}
	}
	for _, n := range aiContainersWriteNames {
		tl := aiContainersTool(t, tools, n)
		if !tl.Mutating || tl.Class != ai.ClassContainers || tl.Gate == nil || tl.Validate == nil {
			t.Errorf("%s: mutating %v class %q gate %v", n, tl.Mutating, tl.Class, tl.Gate != nil)
			continue
		}
		if ask, class := tl.Gate(map[string]any{}); !ask || class != ai.ClassContainers {
			t.Errorf("%s gate = %v %q", n, ask, class)
		}
	}
	ctl := aiContainersTool(t, tools, "containers_control")
	if got := aiParamEnum(ctl.Params["action"]); strings.Join(got, ",") != "start,stop,restart,pause,unpause" {
		t.Errorf("containers_control actions = %v", got)
	}
	if strings.Join(ctl.Required, ",") != "action,target" {
		t.Errorf("containers_control required = %v", ctl.Required)
	}
	if pull := aiContainersTool(t, tools, "images_pull"); strings.Join(pull.Required, ",") != "ref" {
		t.Errorf("images_pull required = %v", pull.Required)
	}
}

// Never offered: remove, prune, push, tag, run, login, logout, context
// use / default, disk usage — no tool in the whole chat list.
func TestAIContainersNeverOfferedTools(t *testing.T) {
	m := &Model{services: newServicesState()}
	call := func(context.Context, aiIDEFunc) (string, error) { return "", nil }
	bad := regexp.MustCompile(`(?i)(remove|_rm|prune|push|_tag|_run|login|logout|context_use|context_default|use_context|disk|system_df)`)
	host := regexp.MustCompile(`^(containers|images|volumes|networks|contexts|registries|compose)_`)
	found := 0
	for _, tl := range m.aiChatTools(t.TempDir(), call) {
		if !host.MatchString(tl.Name) {
			continue
		}
		found++
		if bad.MatchString(tl.Name) {
			t.Errorf("tool %s must not be offered to the model", tl.Name)
		}
	}
	if found != len(aiContainersReadNames)+len(aiContainersWriteNames) {
		t.Errorf("%d container tools in the chat list, want %d", found, len(aiContainersReadNames)+len(aiContainersWriteNames))
	}
	for _, id := range []string{"containers-image-push", "containers-image-tag", "containers-image-run", "containers-image-remove",
		"containers-network-remove", "containers-volume-remove", "containers-volume-disk-usage", "containers-remove",
		"containers-prune", "containers-system-prune", "containers-context-use", "containers-context-default",
		"containers-registry-connect", "containers-registry-signout"} {
		if p := aiActionPolicies[id]; p.Gate != aiGateNever || p.Reason == "" {
			t.Errorf("palette %s: %+v, want never with a reason", id, p)
		}
	}
	if p, ok := aiSettingPolicies["containers_max_inspect_kb"]; !ok || p.Gate != aiGateConfirm || p.Class != ai.ClassSettings {
		t.Errorf("containers_max_inspect_kb policy = %+v %v", p, ok)
	}
}

// "Always allow containers" lasts for this session only: the gate stops
// asking, config.json is not touched, a new session asks again, and a
// "containers" entry typed into ai_auto_approve is ignored.
func TestAIContainersAlwaysIsSessionOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := &Model{services: newServicesState()}
	gate := aiContainersGate(m.services)
	if ask, class := gate(nil); !ask || class != ai.ClassContainers {
		t.Fatalf("gate = %v %q", ask, class)
	}
	reply := make(chan bool, 1)
	m.aiS().confirm = &aiPendingConfirm{tool: ai.Tool{Name: "containers_control", Class: ai.ClassContainers, Mutating: true}, reply: reply}
	m.answerAIToolConfirm("always")
	if !<-reply {
		t.Fatal("always did not allow the call")
	}
	if ask, _ := gate(nil); ask {
		t.Error("still asks after Always allow containers")
	}
	if p, err := ai.ConfigPath(); err == nil {
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), "containers") {
			t.Errorf("config.json got the containers class: %s", b)
		}
	}
	if contains(m.aiS().settings.AutoApprove, ai.ClassContainers) {
		t.Error("the session settings got the containers class")
	}
	fresh := &Model{services: newServicesState()}
	if ask, _ := aiContainersGate(fresh.services)(nil); !ask {
		t.Error("a new session does not ask")
	}
	fresh.aiS().settings.AutoApprove = []string{ai.ClassContainers, ai.ClassGit}
	if set := fresh.aiS().autoApproveSet(); set[ai.ClassContainers] || !set[ai.ClassGit] {
		t.Errorf("autoApproveSet = %v", set)
	}
	if aiContainersGate(nil) == nil {
		t.Fatal("nil state gate")
	}
	if ask, _ := aiContainersGate(nil)(nil); !ask {
		t.Error("no state: must ask")
	}
}

// The card of class containers: Deny · Allow · Always allow containers
// (C-1: Deny is the safe answer, first and plain), with no "Always in this
// project".
func TestAIContainersConfirmCardButtons(t *testing.T) {
	m := &Model{services: newServicesState(), w: 120, h: 40}
	reply := make(chan bool, 1)
	tl := ai.Tool{Name: "containers_control", Class: ai.ClassContainers, Mutating: true}
	m.onAIChatConfirm(aiChatConfirmMsg{tool: tl, human: "Stop container nginx (docker stop aabbccdd)", reply: reply})
	var titles []string
	for _, b := range m.confirm.Buttons() {
		titles = append(titles, b.Title)
	}
	if got := strings.Join(titles, " · "); got != "Deny · Allow · Always allow containers" {
		t.Errorf("buttons = %q", got)
	}
	m.answerAIToolConfirm("deny")
	if <-reply {
		t.Error("deny allowed the call")
	}
}

// Read tools run at once on the fake docker; the list names the engine
// and context, groups by project and says who publishes 8081.
func TestAIContainersListRunsWithoutCard(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	out, err := aiContainersTool(t, m.aiContainersTools(call), "containers_list").Run(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"docker", "context default", "termocode-api-1", "9f3c2e1a1234", "ghcr.io/acme/api:1.4", "running",
		"project termocode", "nginx", "exited", "8081", "port 8081 → termocode-api-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("containers_list has no %q:\n%s", want, out)
		}
	}
	if f.Count("ps --all") != 1 {
		t.Errorf("ps calls: %q", f.Calls())
	}
	for _, c := range f.Calls() {
		for _, bad := range []string{"start", "stop", "rm", "pull", "prune"} {
			if strings.HasPrefix(c, bad) {
				t.Errorf("a read tool ran %q", c)
			}
		}
	}
}

// containers_inspect masks env values (names stay) and secret labels; a
// registered .env secret is masked everywhere; the size is capped.
func TestAIContainersInspectMasksEnv(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	if err := os.WriteFile(filepath.Join(m.services.root, ".env"), []byte("POSTGRES_PASSWORD=hunter2hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.Set(t, "inspect.json", `[{"Id":"9f3c2e1a1234","Name":"/termocode-api-1","Config":{"Env":["POSTGRES_PASSWORD=hunter2hunter2","LANG=C.UTF-8"],`+
		`"Labels":{"com.docker.compose.project":"termocode","vault.token":"s.abcdefghijk"},"Cmd":["./api","--note","hunter2hunter2"]}}]`)
	tools := aiRedactResults(m.aiContainersTools(call))
	out, err := aiContainersTool(t, tools, "containers_inspect").Run(context.Background(), map[string]any{"target": "termocode-api-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"hunter2hunter2", "C.UTF-8", "s.abcdefghijk"} {
		if strings.Contains(out, bad) {
			t.Errorf("inspect leaked %q:\n%s", bad, out)
		}
	}
	for _, want := range []string{"POSTGRES_PASSWORD=[REDACTED]", "LANG=[REDACTED]", `"com.docker.compose.project": "termocode"`, "[REDACTED]"} {
		if !strings.Contains(out, want) {
			t.Errorf("inspect has no %q:\n%s", want, out)
		}
	}
	if f.Count("inspect --type container 9f3c2e1a1234") != 1 {
		t.Errorf("inspect argv: %q", f.Calls())
	}
	// Cap: containers_max_inspect_kb = 1 cuts a big answer.
	dir, _ := configDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"containers_max_inspect_kb": 1}`), 0o644)
	f.Set(t, "inspect.json", `[{"Id":"9f3c2e1a1234","Config":{"Cmd":["`+strings.Repeat("x", 4000)+`"]}}]`)
	out, err = aiContainersTool(t, tools, "containers_inspect").Run(context.Background(), map[string]any{"target": "9f3c2e1a1234"})
	if err != nil || len(out) > 1500 || !strings.Contains(out, "truncated") {
		t.Errorf("cap: %d bytes, %v:\n%.300s", len(out), err, out)
	}
}

// Invalid args are refused before any card, and nothing runs.
func TestAIContainersInvalidArgsRefused(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	tools := m.aiContainersTools(call)
	ctl := aiContainersTool(t, tools, "containers_control")
	for _, a := range []map[string]any{
		{"action": "rm", "target": "nginx"},
		{"action": "kill", "target": "nginx"},
		{"action": "stop", "target": "-f"},
		{"action": "stop", "target": "--all"},
		{"action": "stop", "target": "nginx; rm -rf /"},
		{"action": "stop", "target": "nope"},
		{"action": "stop", "target": ""},
		{"action": "start", "target": "termocode-api-1"}, // already running
		{"action": "unpause", "target": "nginx"},         // not paused
	} {
		if err := ctl.Validate(a); err == nil {
			t.Errorf("containers_control %v accepted", a)
		}
	}
	pull := aiContainersTool(t, tools, "images_pull")
	for _, ref := range []string{"", "-q", "--all-tags", "nginx;id", "$(id)", "NGINX"} {
		if err := pull.Validate(map[string]any{"ref": ref}); err == nil {
			t.Errorf("images_pull %q accepted", ref)
		}
	}
	up := aiContainersTool(t, tools, "compose_up")
	for _, a := range []map[string]any{{"project": "-p"}, {"project": "nope"}, {"project": "termocode", "services": []any{"--build"}}} {
		if err := up.Validate(a); err == nil {
			t.Errorf("compose_up %v accepted", a)
		}
	}
	for _, n := range []string{"containers_inspect", "containers_logs"} {
		if err := aiContainersTool(t, tools, n).Validate(map[string]any{"target": "-v"}); err == nil {
			t.Errorf("%s -v accepted", n)
		}
	}
	for _, c := range f.Calls() {
		if !strings.HasPrefix(c, "ps ") && !strings.HasPrefix(c, "version") && !strings.HasPrefix(c, "context ") {
			t.Errorf("an invalid call ran %q", c)
		}
	}
}

// Allow runs the action on the full id; the card shows name and the short (12 char) id.
func TestAIContainersControlRunsExactArgv(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	ctl := aiContainersTool(t, m.aiContainersTools(call), "containers_control")
	a := map[string]any{"action": "stop", "target": "termocode-api-1"}
	if err := ctl.Validate(a); err != nil {
		t.Fatal(err)
	}
	human := ctl.Describe(a)
	if !strings.Contains(human, "Stop container api-1") || !strings.Contains(human, "docker stop 9f3c2e1a1234") {
		t.Errorf("card text %q", human)
	}
	out, err := ctl.Run(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	var acts []string
	for _, c := range f.Calls() {
		if !strings.HasPrefix(c, "ps ") && !strings.HasPrefix(c, "context ") {
			acts = append(acts, c)
		}
	}
	if strings.Join(acts, "|") != "stop 9f3c2e1a1234" {
		t.Errorf("ran %q, want exactly stop 9f3c2e1a1234", acts)
	}
	if !strings.Contains(out, "stop") || !strings.Contains(out, "context default") {
		t.Errorf("result %q", out)
	}
}

// The session's DOCKER_CONTEXT goes with every call and the result names it.
func TestAIContainersHonourSessionContext(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	m.services.sessionContext = "prod"
	out, err := aiContainersTool(t, m.aiContainersTools(call), "containers_list").Run(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "context prod (this session)") {
		t.Errorf("result does not name the context:\n%s", out)
	}
	calls := f.CtxCalls()
	if len(calls) == 0 {
		t.Fatal("no calls")
	}
	for _, c := range calls {
		if !strings.HasPrefix(c, "prod|") {
			t.Errorf("call without DOCKER_CONTEXT=prod: %q", c)
		}
	}
}

// The system prompt line is there only when an engine is on PATH.
func TestAIContainersPromptLineOnlyWithEngine(t *testing.T) {
	m, _, _ := aiContainersTestModel(t)
	if line := m.aiContainersPromptLine(); !strings.Contains(line, "Containers tools") || !strings.Contains(line, "cannot remove or prune") {
		t.Errorf("with docker: %q", line)
	}
	t.Setenv("PATH", t.TempDir())
	if line := m.aiContainersPromptLine(); line != "" {
		t.Errorf("without an engine: %q", line)
	}
}

// CT-9: the control card shows the name and a 12-char id, never the full
// 64-char id; the call still runs on the full id.
func TestAIContainersControlCardShowsShortID(t *testing.T) {
	m, f, call := aiContainersTestModel(t)
	full := strings.Repeat("9f3c2e1a", 8)
	f.Set(t, "ps.json", `{"ID":"`+full+`","Names":"termocode-api-1","Image":"api","State":"running","Status":"Up","Labels":"com.docker.compose.project=termocode"}`+"\n")
	ctl := aiContainersTool(t, m.aiContainersTools(call), "containers_control")
	a := map[string]any{"action": "stop", "target": "termocode-api-1"}
	human := ctl.Describe(a)
	if strings.Contains(human, full) || !strings.Contains(human, "api-1") || !strings.Contains(human, "docker stop "+full[:12]+")") {
		t.Errorf("card text %q", human)
	}
	if _, err := ctl.Run(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if f.Count("stop "+full) != 1 {
		t.Errorf("stop did not run on the full id: %q", f.Calls())
	}
}
