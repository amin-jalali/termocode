//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/kube/kubetest"
)

// E2E-OPS: the ops features on the real binary, with fakes only (no
// real docker, kubectl or network).
//
// Not here: the update card + rollback. A release build checks GitHub
// over https only, with the release key built in, and the binary has no
// way to point it at a fake release server; that needs a test hook in
// the release binary first (an owner call).

// statsE2E is one `docker stats --no-stream` line for the running
// termocode-api-1 of the shared docker27 testdata.
const statsE2E = `{"ID":"9f3c2e1a1234","Name":"termocode-api-1","CPUPerc":"12.50%","MemUsage":"210MiB / 1GiB","MemPerc":"20.51%","NetIO":"1.5kB / 3kB","BlockIO":"4kB / 8kB","PIDs":"12"}` + "\n"

// sparkRunes are the graph cells (unicode and the ascii icon mode).
const sparkRunes = "▁▂▃▄▅▆▇█_.:-=+*#"

// G-DST: the selected running container shows its CPU, MEM, NET and BLK
// chart rows from `stats --no-stream`, polled again while the view shows.
func TestContainerStatsGraph(t *testing.T) {
	data := fakeDocker(t)
	if err := os.WriteFile(filepath.Join(data, "stats-e2e.json"), []byte(statsE2E), 0o644); err != nil {
		t.Fatal(err)
	}
	tm := Start(t, StartOpts{Cols: 140, Rows: 40})
	tm.settle()
	tm.Palette("Containers: Focus")
	tm.WaitText("CONTAINERS", uiTimeout)
	tm.WaitText("api-1", uiTimeout) // the row under its compose project: termocode › api-1
	for i := 0; i < 30 && !cursorFooterHas(tm, "ghcr.io/acme/api:1.4"); i++ {
		tm.Send(KeyDown)
		tm.settle()
	}
	if !cursorFooterHas(tm, "ghcr.io/acme/api:1.4") {
		t.Fatalf("the cursor never reached the api container:\n%s", tm.Screen())
	}
	tm.WaitFor("the chart rows", uiTimeout, func(ls []string) bool {
		var cpu, mem, net, blk bool
		for _, l := range ls {
			switch {
			case strings.Contains(l, "CPU ") && strings.ContainsAny(l, sparkRunes) && strings.Contains(l, "12%"):
				cpu = true
			case strings.Contains(l, "MEM ") && strings.Contains(l, "210 MB"):
				mem = true
			case strings.Contains(l, "NET in"):
				net = true
			case strings.Contains(l, "BLK r"):
				blk = true
			}
		}
		return cpu && mem && net && blk
	})
	tm.CheckScreen("stats chart", mainFrame)
	argv := filepath.Join(data, "argv.log")
	waitUntil(t, "a second stats call", 15*time.Second, func() bool {
		return strings.Count(fileText(argv), "stats --no-stream") >= 2
	})
	for _, l := range strings.Split(fileText(argv), "\n") {
		if strings.HasPrefix(l, "stats ") && (!strings.Contains(l, "9f3c2e1a1234") || strings.Contains(l, "aabbccdd")) {
			t.Errorf("stats for the wrong containers (only running ones): %q", l)
		}
	}
	tm.Quit()
}

// RC: termocode --container api-1 /app opens a folder inside the fake
// engine's container: the explorer lists it, a file opens with the
// container's text, and a save writes it back into the container.
func TestContainerWorkspaceOnFakeEngine(t *testing.T) {
	root, state := fakeDockerWorkspace(t)
	app := filepath.Join(root, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "hello-rc.txt"), []byte("hello from the container\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm := Start(t, StartOpts{Cols: 120, Rows: 36, Args: []string{"--container", fakeDockerContainer, "/app"}})
	tm.WaitText("hello-rc.txt", 20*time.Second)
	tm.settle()
	tm.CheckScreen("container explorer", mainFrame)

	tm.Palette("View: Focus Explorer")
	tm.settle()
	tm.Send(KeyDown) // from the folder's own row to its file
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("hello from the container", uiTimeout)
	tm.settle()
	tm.Type("Oadded-by-e2e-rc")
	tm.Send(KeyEsc)
	tm.settle()
	tm.Send(KeyCtrlS)
	waitUntil(t, "the save inside the container", 15*time.Second, func() bool {
		return strings.Contains(fileText(filepath.Join(app, "hello-rc.txt")), "added-by-e2e-rc")
	})
	if got := fileText(filepath.Join(app, "hello-rc.txt")); !strings.Contains(got, "hello from the container") {
		t.Errorf("the save lost the old text: %q", got)
	}
	argv := fileText(filepath.Join(state, "argv.log"))
	if !strings.Contains(argv, "cp ") {
		t.Errorf("no `docker cp` call:\n%s", argv)
	}
	tm.Quit()
}

// G-K8S: the Kubernetes view with a fake kubectl lists the pods of the
// current context and namespace, shows its keys, and l on a pod follows
// its logs in a terminal tab. Nothing writes to the cluster.
func TestKubernetesViewFakeKubectl(t *testing.T) {
	f := kubetest.New(t)
	f.Fixtures(t, filepath.Join("..", "internal", "kube", "testdata"))
	f.Set(t, "logs.txt", "kube-e2e-log-line-7\n")
	t.Setenv("PATH", f.PathEnv())
	tm := Start(t, StartOpts{Cols: 140, Rows: 40, Env: []string{"KUBECONFIG=" + filepath.Join(t.TempDir(), "kubeconfig")}})
	tm.settle()
	tm.Palette("Kubernetes: Show")
	tm.WaitText("KUBERNETES", uiTimeout)
	tm.WaitText("PODS", uiTimeout)
	tm.WaitText("x2x4q", uiTimeout) // api-7d9f8c6b5-x2x4q, cut in the middle in the side bar
	tm.settle()
	tm.CheckScreen("kubernetes", mainFrame)
	if f.Count("get pods -o json --context dev --namespace shop") == 0 {
		t.Errorf("no scoped get pods in %v", f.Calls())
	}

	tm.Send("?")
	tm.WaitText("Kubernetes keys", uiTimeout)
	tm.Send(KeyEsc)
	tm.WaitGone("Kubernetes keys", uiTimeout)

	// Down to the first pod (its footer names the pod keys), then logs.
	for i := 0; i < 10 && !cursorFooterHas(tm, "l logs"); i++ {
		tm.Send("j")
		tm.settle()
	}
	if !cursorFooterHas(tm, "l logs") {
		t.Fatalf("the cursor never reached a pod:\n%s", tm.Screen())
	}
	tm.Send("l")
	tm.WaitFor("the logs or the container picker", uiTimeout, func(ls []string) bool {
		s := strings.Join(ls, "\n")
		return strings.Contains(s, "Logs of Container") || strings.Contains(s, "kube-e2e-log-line-7")
	})
	if tm.Has("Logs of Container") { // a pod with two containers asks which
		tm.settle()
		tm.Send(KeyEnter)
	}
	tm.WaitText("kube-e2e-log-line-7", 15*time.Second)
	if f.Count("logs ") == 0 {
		t.Errorf("no kubectl logs call in %v", f.Calls())
	}
	for _, c := range f.Calls() {
		for _, w := range []string{"delete", "rollout restart", "scale", "apply", "cordon", "drain", "set image"} {
			if strings.HasPrefix(c, w) || strings.Contains(c, " "+w+" ") {
				t.Errorf("a read-only walk wrote to the cluster: %q", c)
			}
		}
	}
	tm.Quit()
}

// G-RST: a terminal tab open at quit comes back after a restart, with a
// fresh shell in the folder it was in.
func TestTerminalsBackAfterRestart(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not on PATH (the folder comes from its shell integration)")
	}
	ws := NewRepo(t)
	if err := os.MkdirAll(filepath.Join(ws, "sub-rst"), 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	opts := StartOpts{Home: home, Workspace: ws, Cols: 120, Rows: 36, Env: []string{"SHELL=" + bash}}

	tm := Start(t, opts)
	tm.WaitText("README.md", uiTimeout)
	tm.OpenTerminal()
	tm.Type("cd sub-rst && echo first-$((20+1))\r")
	tm.WaitText("first-21", uiTimeout)
	tm.settle()
	tm.Quit() // the panel stays open: its tab is kept in the session

	tm = Start(t, opts)
	tm.WaitFor("the restored shell prompt", 20*time.Second, func(ls []string) bool {
		for _, l := range ls {
			if strings.HasSuffix(l, "$") {
				return true
			}
		}
		return false
	})
	tm.settle()
	if tm.Has("first-21") {
		t.Errorf("the old output came back; only the folder should:\n%s", tm.Screen())
	}
	tm.ClickText("sub-rst$") // the editor has the focus at start: into the terminal
	tm.settle()
	tm.Type("echo in-$(basename \"$PWD\")\r")
	tm.WaitText("in-sub-rst", uiTimeout)
	tm.CheckScreen("terminal restored", mainFrame)
	tm.Quit()
}
