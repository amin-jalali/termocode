//go:build unix

package session

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Spec (MOB-51): frameStart starts the UI with no terminal attached; the
// reply is the session's info, which says headless and names the UI's
// pid. The door's idle minutes reach the UI as EnvDoorIdle, only inside
// 5..240.
func TestHandleStartHeadless(t *testing.T) {
	out := filepath.Join(t.TempDir(), "env")
	s := inprocServer(t, `printf '%s' "$`+EnvDoorIdle+`" > `+out+`; sleep 30`)
	cli, done := served(t, s)
	if err := writeFrame(cli, frameStart, encodeCode(60)); err != nil {
		t.Fatal(err)
	}
	typ, b, err := readFrame(cli)
	if err != nil || typ != frameStatus {
		t.Fatalf("reply %q %v %s", typ, err, b)
	}
	var in Info
	if err := json.Unmarshal(b, &in); err != nil {
		t.Fatal(err)
	}
	if !in.Headless || in.UIPID == 0 || in.Attached {
		t.Fatalf("info %+v", in)
	}
	waitDone(t, done)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if v, err := os.ReadFile(out); err == nil && string(v) == "60" {
			break
		}
		if time.Now().After(deadline) {
			v, _ := os.ReadFile(out)
			t.Fatalf("UI env %s = %q, want 60", EnvDoorIdle, v)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Spec (MOB-51): a second start leaves the running UI as it is; an idle
// value outside the range passes no env at all.
func TestStartHeadlessTwiceKeepsTheUI(t *testing.T) {
	s := inprocServer(t, "sleep 30")
	if err := s.startHeadless(4); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	first := s.cmd
	env := strings.Join(first.Env, "\n")
	s.mu.Unlock()
	if strings.Contains(env, EnvDoorIdle+"=") {
		t.Fatalf("idle 4 (below %d) passed %s", DoorIdleMin, EnvDoorIdle)
	}
	if err := s.startHeadless(60); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != first {
		t.Fatal("a second start replaced the UI")
	}
}

// Spec (MOB-51): a session that is ending refuses a start with a reason
// (frameDetached).
func TestHandleStartHeadlessWhileEnding(t *testing.T) {
	s := inprocServer(t, "exit 0")
	s.exiting = true
	cli, done := served(t, s)
	if err := writeFrame(cli, frameStart, encodeCode(0)); err != nil {
		t.Fatal(err)
	}
	typ, b, err := readFrame(cli)
	if err != nil || typ != frameDetached || !strings.Contains(string(b), "ending") {
		t.Fatalf("reply %q %v %q", typ, err, b)
	}
	waitDone(t, done)
}

// Spec (MOB-51): WaitWebHub waits for the web hub socket; with fresh set
// a socket older than the start does not count.
func TestWaitWebHub(t *testing.T) {
	dir, err := os.MkdirTemp("", "tcw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	p := Paths{WebSocket: filepath.Join(dir, "w.sock"), Log: filepath.Join(dir, "l")}
	if err := WaitWebHub(p, time.Time{}, 150*time.Millisecond); err == nil {
		t.Fatal("no socket: want an error")
	}
	ln, err := net.Listen("unix", p.WebSocket)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := WaitWebHub(p, time.Time{}, time.Second); err != nil {
		t.Fatalf("socket there: %v", err)
	}
	if err := WaitWebHub(p, time.Now().Add(time.Hour), 150*time.Millisecond); err == nil {
		t.Fatal("an old socket counted as fresh")
	}
}

// Spec (MOB-51): StartHeadless starts a session with its UI and no
// terminal (started true); a second call finds the UI running and reports
// started false. UIRunning is true only once the web hub socket exists.
func TestStartHeadlessThroughASpawnedServer(t *testing.T) {
	r := newRig(t, "sleep 30")
	spawn := func() error { _, err := r.spawn(); return err }
	if UIRunning(r.p) {
		t.Fatal("nothing runs yet")
	}
	started, err := StartHeadless(r.p, 0, spawn)
	if err != nil || !started {
		t.Fatalf("first start: %v %v", started, err)
	}
	if UIRunning(r.p) {
		t.Fatal("the UI runs but its web hub socket is not there yet")
	}
	ln, err := net.Listen("unix", r.p.WebSocket)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if !UIRunning(r.p) {
		t.Fatal("UI and hub socket are there")
	}
	started, err = StartHeadless(r.p, 0, spawn)
	if err != nil || started {
		t.Fatalf("second start: %v %v", started, err)
	}
	// no server and none can be spawned: an error
	if _, err := StartHeadless(Paths{Socket: filepath.Join(t.TempDir(), "none.sock")}, 0, func() error { return os.ErrPermission }); err == nil {
		t.Error("a failing spawn must fail")
	}
}
