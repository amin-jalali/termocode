package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/kube"
)

func TestKubeRolloutReadsStatusAndHistory(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "rollout-status.txt", "deployment \"api\" successfully rolled out\n")
	f.Set(t, "rollout-history.txt", "deployment.apps/api\nREVISION  CHANGE-CAUSE\n1         <none>\n2         image v2\n3         image v3\n")
	kubeCursorTo(t, m, "deployments", "api")
	cmd := kubeKey(t, m, "o")
	if cmd == nil {
		t.Fatal("o ran nothing")
	}
	msg := cmd().(kubeRolloutMsg)
	if msg.err != nil || len(msg.revs) != 3 || !strings.Contains(msg.status, "rolled out") {
		t.Fatalf("rollout = %+v", msg)
	}
	if f.Count("rollout undo") != 0 {
		t.Error("o changed something")
	}
	items := kubeRolloutItems(msg)
	if len(items) != 4 || items[0].ID != "k8s:rollout-page:api" {
		t.Fatalf("items = %+v", items)
	}
	// newest first; the newest is the current one and has no undo
	if items[1].ID != "k8s:rollout-current:api" || items[1].Hint != "current" {
		t.Errorf("current row = %+v", items[1])
	}
	if items[2].ID != "k8s:undo:api:2" || !strings.Contains(items[2].Title, "image v2") {
		t.Errorf("revision row = %+v", items[2])
	}
}

func TestKubeUndoAsksFirstWithCancelFocused(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	if cmd := m.handleKubePickerSelect("k8s:undo:api:2"); cmd != nil {
		t.Error("the pick ran something before the card")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindKubeUndo {
		t.Fatal("no undo card")
	}
	b := m.confirm.Buttons()
	if len(b) != 2 || b[0].ID != "cancel" || m.confirm.SafeID() != "cancel" {
		t.Errorf("buttons %+v safe %q", b, m.confirm.SafeID())
	}
	m.confirmOpen = false
	if cmd := m.handleConfirmSelect("cancel"); cmd != nil || f.Count("rollout undo") != 0 {
		t.Fatal("cancel undid")
	}
	m.handleKubePickerSelect("k8s:undo:api:2")
	m.confirmOpen = false
	cmd := m.handleConfirmSelect("undo")
	if cmd == nil {
		t.Fatal("no undo command")
	}
	msg := cmd().(kubeUndoMsg)
	if msg.err != nil || f.Count("rollout undo deployment/api --to-revision=2 --context dev --namespace shop") != 1 {
		t.Errorf("msg %+v calls %v", msg, f.Calls())
	}
}

func TestKubeSetImagePromptThenCard(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	for i := range m.kube.deploys {
		if m.kube.deploys[i].Name == "api" {
			m.kube.deploys[i].Images = []kube.ContainerImage{{Name: "api", Image: "shop/api:v3"}}
		}
	}
	kubeCursorTo(t, m, "deployments", "api")
	if cmd := kubeKey(t, m, "I"); cmd != nil {
		t.Error("I ran something")
	}
	if !m.promptOpen || m.promptKind != promptKindKubeImage {
		t.Fatal("no image prompt")
	}
	if got := m.prompt.Value(); got != "api=shop/api:v3" {
		t.Errorf("prompt starts at %q", got)
	}
	m.promptOpen = false
	m.handlePromptSubmit("bad image")
	if !m.promptOpen || m.confirmOpen {
		t.Fatal("a bad answer did not ask again")
	}
	m.promptOpen = false
	m.handlePromptSubmit("api=shop/api:v4")
	if !m.confirmOpen || m.confirmKind != confirmKindKubeImage || m.confirm.SafeID() != "cancel" {
		t.Fatal("no Cancel-first image card")
	}
	if f.Count("set image") != 0 {
		t.Fatal("set before the answer")
	}
	m.confirmOpen = false
	cmd := m.handleConfirmSelect("set")
	if cmd == nil {
		t.Fatal("no set image command")
	}
	msg := cmd().(kubeImageMsg)
	if msg.err != nil || f.Count("set image deployment/api api=shop/api:v4 --context dev --namespace shop") != 1 {
		t.Errorf("msg %+v calls %v", msg, f.Calls())
	}
}

func TestKubeParseImageAnswer(t *testing.T) {
	d := kube.Deployment{Name: "api", Images: []kube.ContainerImage{{Name: "api", Image: "a:1"}, {Name: "side", Image: "b:1"}}}
	if c, img, err := kubeParseImageAnswer(d, "side=b:2"); err != nil || c != "side" || img != "b:2" {
		t.Errorf("got %q %q %v", c, img, err)
	}
	if _, _, err := kubeParseImageAnswer(d, "nope=b:2"); err == nil {
		t.Error("an unknown container passed")
	}
	if c, _, err := kubeParseImageAnswer(kube.Deployment{Images: []kube.ContainerImage{{Name: "x"}}}, "y:2"); err != nil || c != "x" {
		t.Errorf("one container, no name: %q %v", c, err)
	}
}
