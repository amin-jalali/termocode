package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/source"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// Spec (plan §3.2 notify, WFX-18): "notify": ["message:<step id>"] sends
// the run's summary through that message step's channel when the run
// ends. On the OS or the browsers it goes at once; a webhook is a write
// to an outside system, so it goes only when that step's writes are
// allowed for this file version, else the user is told how.
func TestWorkflowsNotifyMessageTarget(t *testing.T) {
	m, _ := wfxRig(t)
	var osGot []string
	prev := deliverOSNotification
	deliverOSNotification = func(body string) { osGot = append(osGot, body) }
	t.Cleanup(func() { deliverOSNotification = prev })
	var posts []string
	prevPost, prevSec := wfxWebhookPost, wfxMessageSecrets
	wfxWebhookPost = func(_ context.Context, url string, body []byte) error {
		posts = append(posts, url+" "+string(body))
		return nil
	}
	wfxMessageSecrets = func() (source.SecretGetter, error) { return wfxTestSecrets{"HOOK": "https://hooks.example.test/x"}, nil }
	t.Cleanup(func() { wfxWebhookPost, wfxMessageSecrets = prevPost, prevSec })
	m.termBlurred = true
	write := func(name, via string) {
		body := `{ "version": 1, "name": "` + name + `", "notify": ["message:ping"],
  "steps": [
    { "id": "docs", "type": "source", "from": "files", "glob": "docs/**/*.md" },
    { "id": "ping", "type": "write", "to": "message", "via": "` + via + `", "text": "hi", "when": "len(steps.docs.items) > 100" }
  ] }`
		if err := os.WriteFile(filepath.Join(wfxPrivateDir(), name+".jsonc"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("pinged", "os")
	write("hooked", "${secret:HOOK}")
	m.wfxLoad("")
	st := m.wfxS()
	runIt := func(name string) *run.RunFile {
		t.Helper()
		e, ok := wire.Find(st.list, name)
		if !ok || e.Broken() {
			t.Fatalf("%s: %+v", name, e.Problems)
		}
		id, _, _, err := m.wfxStartID(e, false)
		if err != nil {
			t.Fatal(err)
		}
		var cmds []func()
		rf := wfxPumpCmds(t, m, id, &cmds)
		for _, f := range cmds {
			f()
		}
		return rf
	}
	if rf := runIt("pinged"); rf.State != run.RunOK {
		t.Fatalf("pinged: %s", rf.State)
	}
	if len(osGot) != 1 || !strings.HasPrefix(osGot[0], "pinged: done") {
		t.Fatalf("OS notification %q", osGot)
	}

	runIt("hooked")
	if len(posts) != 0 {
		t.Fatalf("a webhook got the summary without an allowed write: %q", posts)
	}
	if txt := toastText(*m); !strings.Contains(txt, "ping") || !strings.Contains(txt, "Allow for this workflow") {
		t.Fatalf("toast %q", txt)
	}
	e, _ := wire.Find(st.list, "hooked")
	if err := ledger.AllowStep(st.root, "hooked", e.Hash, "ping"); err != nil {
		t.Fatal(err)
	}
	m.wfxLoad("")
	runIt("hooked")
	if len(posts) != 1 || !strings.HasPrefix(posts[0], `https://hooks.example.test/x {"text":"hooked: done`) {
		t.Fatalf("webhook posts %q", posts)
	}
}

type wfxTestSecrets map[string]string

func (f wfxTestSecrets) Get(name string) (string, bool, error) {
	v, ok := f[name]
	return v, ok, nil
}
