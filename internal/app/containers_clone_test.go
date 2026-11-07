package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
)

func TestRunOptionWordsFillTheOptions(t *testing.T) {
	o := containers.RunOptions{Image: "alpine:3", Remove: true}
	err := parseRunOptionsLine(`volume=/data:/data network=back restart=always user=1000 workdir=/app label=team=core detach no-rm -- sh -c "echo hi"`, &o)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(o.Volumes, []string{"/data:/data"}) || o.Network != "back" || o.Restart != "always" || o.User != "1000" ||
		o.Workdir != "/app" || o.Labels["team"] != "core" || !o.Detach || o.Remove || !slices.Equal(o.Command, []string{"sh", "-c", "echo hi"}) {
		t.Fatalf("options %+v", o)
	}
	for _, bad := range []string{"bogus=1", "volume", "label=novalue", `user="a`} {
		o := containers.RunOptions{Image: "alpine:3"}
		if err := parseRunOptionsLine(bad, &o); err == nil {
			t.Fatalf("%q: want an error", bad)
		}
	}
}

func TestDuplicateContainerCardIsCancelFirstWithACopyName(t *testing.T) {
	m := svcTestModel(svcHostState(), 120, 40)
	o := containers.RunOptions{Image: "nginx:1.27", Name: "nginx", Detach: true, Env: map[string]string{"K": "v"}}
	m.onContainerClone(containerCloneMsg{op: "duplicate", id: "c5", opts: o})
	if !m.confirmOpen || m.confirmKind != confirmKindContainerClone {
		t.Fatal("no duplicate card")
	}
	b := m.confirm.Buttons()
	if b[0].ID != "cancel" || b[len(b)-1].ID != "duplicate" {
		t.Fatalf("buttons %+v", b)
	}
	card := strings.Join(strings.Fields(ansi.Strip(m.confirm.View())), " ")
	if !strings.Contains(card, "--name nginx-copy") || strings.Contains(card, "K=v") {
		t.Fatalf("card %q", card)
	}
	m.answerContainerClone("cancel")
	if m.services.clonePending.line != "" {
		t.Fatal("cancel must drop the plan")
	}
}

func TestRecreateContainerCardIsDestructive(t *testing.T) {
	m := svcTestModel(svcHostState(), 120, 40)
	m.onContainerClone(containerCloneMsg{op: "recreate", id: "c5", opts: containers.RunOptions{Image: "nginx:1.27", Name: "nginx", Detach: true}})
	b := m.confirm.Buttons()
	if b[0].ID != "cancel" || b[len(b)-1].Style != confirm.StyleDestructive {
		t.Fatalf("buttons %+v", b)
	}
	if !strings.Contains(m.services.clonePending.line, "docker rm -f c5 && docker run") {
		t.Fatalf("line %q", m.services.clonePending.line)
	}
}

func TestRunImageCardOffersMoreOptions(t *testing.T) {
	m := svcTestModel(svcHostState(), 120, 40)
	m.pendingImageRef = "alpine:3"
	m.onContainerImagePrompt(promptKindContainerRunName, "demo")
	m.onContainerImagePrompt(promptKindContainerRunPorts, "")
	m.onContainerImagePrompt(promptKindContainerRunEnv, "A=1")
	m.onContainerImagePrompt(promptKindContainerRunEnv, "")
	ids := []string{}
	for _, b := range m.confirm.Buttons() {
		ids = append(ids, b.ID)
	}
	if !slices.Equal(ids, []string{"cancel", "run-options", "run"}) {
		t.Fatalf("buttons %q", ids)
	}
	m.handleServicesConfirm("run-options")
	if !m.promptOpen || m.promptKind != promptKindContainerRunOptions {
		t.Fatal("More options must open the options prompt")
	}
	m.onRunOptionsPrompt("network=back -- sleep 5")
	joined := strings.Join(m.pendingHostArgs, " ")
	if !strings.Contains(joined, "--network back") || !strings.Contains(joined, "-e A") || strings.Contains(joined, "A=1") || !strings.HasSuffix(joined, "alpine:3 sleep 5") {
		t.Fatalf("args %q", joined)
	}
}
