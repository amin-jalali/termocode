//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeSSHScript stands in for ssh in the termocode ssh tests (no real
// host, no network): it logs its arguments, fails like an unreachable
// host while a "fail" file exists, and runs the remote command (after
// --) here, in the background, so a TERM to it can play a dropped link:
// the far termocode dies at once (no goodbye) and ssh exits 255.
const fakeSSHScript = `#!/bin/sh
d='%DIR%'
echo "$*" >> "$d/argv"
echo $$ > "$d/ssh.pid"
if [ -e "$d/fail" ]; then echo "ssh: connect to host box port 22: Network is unreachable"; exit 255; fi
while [ "$1" != "--" ]; do shift; done; shift
exec 3<&0
sh -c "exec $*" <&3 &
child=$!
echo $child > "$d/remote.pid"
trap 'kill -KILL $child 2>/dev/null; exit 255' TERM
wait $child
exit $?
`

// sshRig is a sessionRig reached through termocode ssh and a fake ssh.
type sshRig struct {
	*sessionRig
	dir string // the fake ssh's folder
}

func newSSHRig(t *testing.T) *sshRig {
	t.Helper()
	r := &sshRig{sessionRig: newSessionRig(t), dir: t.TempDir()}
	script := strings.ReplaceAll(fakeSSHScript, "%DIR%", r.dir)
	if err := os.WriteFile(filepath.Join(r.dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return r
}

// start runs `termocode ssh --dir <ws> box` and waits for the editor.
func (r *sshRig) start() *Term {
	r.t.Helper()
	env := append([]string{}, r.env...)
	env = append(env,
		"PATH="+r.dir+string(os.PathListSeparator)+filepath.Dir(binPath)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TERMOCODE_LINK_TIMING=200ms,1500ms")
	tm := Start(r.t, StartOpts{Home: r.home, Workspace: r.ws, Env: env, Args: []string{"ssh", "--dir", r.ws, "box"}})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	return tm
}

func (r *sshRig) pid(name string) int {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dir, name))
	if err != nil {
		r.t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid
}

func (r *sshRig) runs() int {
	b, _ := os.ReadFile(filepath.Join(r.dir, "argv"))
	return strings.Count(string(b), "\n")
}

func (r *sshRig) set(name string, on bool) {
	p := filepath.Join(r.dir, name)
	if on {
		_ = os.WriteFile(p, nil, 0o644)
	} else {
		_ = os.Remove(p)
	}
}

// end ends the session; termocode ssh then exits by itself.
func (r *sshRig) end(tm *Term) {
	r.t.Helper()
	if err := r.run("kill"); err != nil {
		r.t.Errorf("kill: %v", err)
	}
	select {
	case <-tm.exited:
	case <-time.After(15 * time.Second):
		r.t.Errorf("termocode ssh did not exit after the session ended")
	}
}

// TestSSHReconnectAfterDrop (G-NET): the ssh link drops while the user
// works; termocode ssh keeps the screen, shows the offline line, pauses
// typing, and comes back by itself when the far side can be reached
// again: the same session, a full redraw, a toast that says so and that
// keys typed offline were dropped.
func TestSSHReconnectAfterDrop(t *testing.T) {
	r := newSSHRig(t)
	tm := r.start()
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Type("ounsaved-marker-42")
	tm.WaitText("unsaved-marker-42", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()

	r.set("fail", true)
	if err := syscall.Kill(r.pid("ssh.pid"), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	tm.WaitText("Reconnecting", uiTimeout)
	tm.WaitText("typing is paused", uiTimeout)
	if !tm.Has("unsaved-marker-42") {
		t.Errorf("the screen was not kept while offline:\n%s", tm.Screen())
	}
	tm.Type("lost-keys")
	tm.WaitFor("a retry", uiTimeout, func([]string) bool { return r.runs() >= 3 })
	r.set("fail", false)

	tm.WaitText("Reconnected after", 20*time.Second)
	tm.WaitText("keys typed while offline were dropped", uiTimeout)
	tm.WaitGone("Reconnecting", uiTimeout)
	if !tm.Has("unsaved-marker-42") {
		t.Errorf("not the same session after the reconnect:\n%s", tm.Screen())
	}
	if tm.Has("lost-keys") {
		t.Errorf("keys typed offline reached the editor")
	}
	r.end(tm)
}

// TestSSHReconnectFrozenLink (G-NET): the link freezes (no error, just
// silence, as after a Wi-Fi switch); the heartbeat gives it up and
// termocode ssh connects again by itself.
func TestSSHReconnectFrozenLink(t *testing.T) {
	r := newSSHRig(t)
	tm := r.start()
	remote := r.pid("remote.pid")
	if err := syscall.Kill(remote, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(remote, syscall.SIGKILL) })
	tm.WaitText("Reconnected", 20*time.Second)
	if n := r.runs(); n < 2 {
		t.Errorf("%d ssh runs, want a second one", n)
	}
	r.end(tm)
}
