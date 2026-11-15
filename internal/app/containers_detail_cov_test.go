package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

const ctrInspectFull = `[{"Id":"c5abcdef","Name":"/nginx","RestartCount":2,
 "State":{"Status":"running","ExitCode":0,"Health":{"Status":"healthy","FailingStreak":0,"Log":[{"Start":"t1","ExitCode":0,"Output":"ok|fine"}]}},
 "Config":{"Env":["A=secret","B=x"],"Labels":{"app":"web"}},
 "Mounts":[{"Type":"volume","Name":"data","Source":"/var/lib/x","Destination":"/data","RW":true},{"Type":"bind","Source":"/etc/x","Destination":"/x","RW":false}]}]`

// I on a container reads inspect and top from the fake docker and opens
// the detail page; a stopped container has no processes; an inspect
// failure toasts.
func TestCovContainerDetailPage(t *testing.T) {
	m, x := ctrModelScripted(t)
	x.on("inspect c5", ctrInspectFull)
	x.on("top c5", "UID PID CMD\nroot 1 nginx -g daemon off;\n")
	cursorOnContainer(t, m, "c5")
	if ctrUI(t, m, ctrKey(m, "I")) != nil {
		t.Fatalf("detail: %s", ctrToast(m))
	}
	// The page text itself.
	x.on("inspect c3", `{"Id":"c3","Name":"","State":{"Status":"exited","ExitCode":1}}`)
	_ = ctrUI(t, m, m.showContainerDetail(m.services.containerRows[2]))
	x.fail("top c5", "no ps in the image")
	_ = ctrUI(t, m, m.showContainerDetail(m.services.containerRows[4]))
	x.fail("inspect c5", "gone")
	_ = ctrUI(t, m, m.showContainerDetail(m.services.containerRows[4]))
	if !strings.Contains(ctrToast(m), "Cannot read nginx") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	ctrNvim(t, m)
	x.on("inspect c1", ctrInspectFull)
	if ctrUI(t, m, m.showContainerDetail(m.services.containerRows[0])) != nil {
		t.Fatalf("page with an editor: %s", ctrToast(m))
	}
}

// The Markdown of the page: every part filled, and every part empty.
func TestCovContainerDetailMarkdown(t *testing.T) {
	d, err := containers.ParseDetail([]byte(ctrInspectFull))
	if err != nil {
		t.Fatal(err)
	}
	top := containers.ParseTop("UID PID CMD\nroot 1 nginx\n")
	page := containerDetailPage(d, top, "")
	for _, want := range []string{"# nginx", "Restarts: 2", "- Status: healthy", `ok\|fine`, "| root | 1 | nginx |", "- A", "Values are not shown.", "| volume | data | /data | read-write |", "| bind | /etc/x | /x | read-only |", "| app | web |"} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "secret") {
		t.Fatal("an env value is on the page")
	}
	empty := containerDetailPage(containers.Detail{ID: "0123456789abcdef"}, containers.TopTable{}, "")
	for _, want := range []string{"# 0123456789ab", "No health check.", "no processes", "## Environment names\n\nNone.", "## Mounts\n\nNone.", "## Labels\n\nNone."} {
		if !strings.Contains(empty, want) {
			t.Errorf("empty page lacks %q:\n%s", want, empty)
		}
	}
}

// ctrModelScripted is ctrModel with a scripted engine.
func ctrModelScripted(t *testing.T) (*Model, *ctrExec) {
	t.Helper()
	m, _ := ctrModel(t)
	return m, ctrScripted(t, m)
}
