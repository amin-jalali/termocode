//go:build e2e && record

package e2e

// The hero GIF of assets/gifs: the real binary in a PTY, a real
// persistent session, a real disconnect and a real reattach. Frames are
// what the VT emulator shows, sampled while it happens; nothing is edited.
//
//	go run ./cmd/recordgifs -only hero
//
// runs this test (GIF_FRAMES_DIR is where it writes frames.json and the
// .ans screens; cmd/recordgifs encodes them).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// heroModel is the demo model of the hero: it answers the task in two
// parts. The first part streams while the client is attached; the second
// waits for release (the client is gone by then) and finishes the answer.
type heroModel struct {
	srv     *httptest.Server
	release chan struct{}
	started chan struct{}
	served  chan struct{}
	once    [3]sync.Once
}

const (
	heroPart1 = "I looked at the users table. Most queries filter by plan, and plan has no index, so each one reads every row. "
	heroPart2 = "I wrote the fix: CREATE INDEX users_plan_idx ON users (plan); It speeds up the query on your screen. Run it on the server when you are ready."
)

func newHeroModel(t *testing.T) *heroModel {
	t.Helper()
	hm := &heroModel{release: make(chan struct{}), started: make(chan struct{}), served: make(chan struct{})}
	hm.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		send := func(text string) {
			b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": text}}}})
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		}
		hm.once[0].Do(func() { close(hm.started) })
		for _, wd := range strings.SplitAfter(heroPart1, " ") {
			if wd != "" {
				send(wd)
				time.Sleep(110 * time.Millisecond)
			}
		}
		select {
		case <-hm.release:
		case <-r.Context().Done():
			return
		}
		for _, wd := range strings.SplitAfter(heroPart2, " ") {
			if wd != "" {
				send(wd)
				time.Sleep(15 * time.Millisecond)
			}
		}
		b, _ := json.Marshal(map[string]any{"choices": []any{}, "model": "demo",
			"usage": map[string]any{"prompt_tokens": 900, "completion_tokens": 200}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
		fl.Flush()
		hm.once[1].Do(func() { close(hm.served) })
	}))
	t.Cleanup(func() {
		select {
		case <-hm.release:
		default:
			close(hm.release)
		}
		hm.srv.Close()
	})
	return hm
}

// heroWorkspace is a small shop project: Go code, a SQL file and a SQLite
// database with sample users, in a git repository.
func heroWorkspace(t *testing.T) string {
	t.Helper()
	parent, err := os.MkdirTemp("", "demo-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(parent) })
	ws := filepath.Join(parent, "shop-api")
	write := func(name, body string) {
		p := filepath.Join(ws, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "# shop-api\n\nA small API for the shop.\n")
	write("main.go", "package main\n\nimport (\n\t\"fmt\"\n\t\"net/http\"\n)\n\nfunc main() {\n\thttp.HandleFunc(\"/users\", listUsers)\n\tfmt.Println(\"listening on :8080\")\n\thttp.ListenAndServe(\":8080\", nil)\n}\n\nfunc listUsers(w http.ResponseWriter, r *http.Request) {\n\tfmt.Fprintln(w, \"ada, grace, linus\")\n}\n")
	write("go.mod", "module example.com/shop-api\n\ngo 1.22\n")
	write(".termocode/databases.json", `{"connections":[{"id":"app","name":"app","driver":"sqlite","path":"app.db"}]}`)
	sqlite := requireSQLite(t)
	seed := "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL, plan TEXT);" +
		"INSERT INTO users (email, plan) VALUES ('ada@example.com','pro'),('grace@example.com','free'),('linus@example.com','team'),('mary@example.com','pro'),('ken@example.com','free');"
	if out, err := exec.Command(sqlite, filepath.Join(ws, "app.db"), seed).CombinedOutput(); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = ws
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Sample Dev", "GIT_AUTHOR_EMAIL=dev@example.com",
			"GIT_COMMITTER_NAME=Sample Dev", "GIT_COMMITTER_EMAIL=dev@example.com", "GIT_CONFIG_NOSYSTEM=1", "HOME="+ws)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-q", "-m", "init: users API")
	return ws
}

// heroRecorder samples the screen of the current terminal while the
// scenes run; each distinct screen is a frame that lasts until the next.
type heroRecorder struct {
	mu      sync.Mutex
	cur     *Term
	samples []heroSample
	phases  []heroPhase
	t0      time.Time
	stop    chan struct{}
	done    chan struct{}
}

type heroSample struct {
	at   time.Duration
	ansi string
}

type heroPhase struct {
	at      time.Duration
	caption string
	target  time.Duration
}

func (r *heroRecorder) now() time.Duration { return time.Since(r.t0) }

func (r *heroRecorder) run() {
	defer close(r.done)
	tick := time.NewTicker(70 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-tick.C:
		}
		r.mu.Lock()
		tm := r.cur
		r.mu.Unlock()
		if tm == nil {
			continue
		}
		tm.mu.Lock()
		s := tm.em.Render()
		tm.mu.Unlock()
		r.mu.Lock()
		if n := len(r.samples); n == 0 || r.samples[n-1].ansi != s {
			r.samples = append(r.samples, heroSample{r.now(), s})
		}
		r.mu.Unlock()
	}
}

// attach makes tm the terminal being recorded.
func (r *heroRecorder) attach(tm *Term) {
	r.mu.Lock()
	r.cur = tm
	r.mu.Unlock()
}

// phase starts a scene: its caption and the time it gets in the GIF.
func (r *heroRecorder) phase(caption string, target time.Duration) {
	r.mu.Lock()
	r.phases = append(r.phases, heroPhase{r.now(), caption, target})
	r.mu.Unlock()
}

func sleep(d time.Duration) { time.Sleep(d) }

// typeSlow types like a quick typist: one key every 30 ms.
func (tm *Term) typeSlow(s string) {
	tm.t.Helper()
	for _, r := range s {
		tm.Send(string(r))
		time.Sleep(30 * time.Millisecond)
	}
}

// hangUp is what sshd does when the connection dies: SIGHUP to the client
// (its terminal is gone). It waits for the client to exit, so the session
// has seen the client leave before the next client attaches.
func (tm *Term) hangUp() {
	tm.t.Helper()
	_ = tm.cmd.Process.Signal(syscall.SIGHUP)
	select {
	case <-tm.exited:
	case <-time.After(10 * time.Second):
		tm.t.Fatalf("client did not exit after SIGHUP")
	}
	select {
	case <-tm.readDone:
	case <-time.After(3 * time.Second):
	}
	_ = tm.ptmx.Close()
}

// clickTab clicks the tab (row 0) that shows text.
func (tm *Term) clickTab(text string) {
	tm.t.Helper()
	l := tm.Lines()[0]
	i := strings.Index(l, text)
	if i < 0 {
		tm.t.Fatalf("tab %q not in %q", text, l)
	}
	tm.Click(len([]rune(l[:i])), 0)
}

func TestRecordHero(t *testing.T) {
	out := os.Getenv("GIF_FRAMES_DIR")
	if out == "" {
		t.Skip("GIF_FRAMES_DIR not set (go run ./cmd/recordgifs)")
	}
	requireSQLite(t)
	hm := newHeroModel(t)
	ws := heroWorkspace(t)
	rt, err := os.MkdirTemp("", "tcgif")
	if err != nil {
		t.Fatal(err)
	}
	r := &sessionRig{
		t: t, home: t.TempDir(), ws: ws, runtime: rt,
		env: []string{"XDG_RUNTIME_DIR=" + rt, "SSH_CONNECTION=10.0.0.1 50000 10.0.0.2 22",
			"TERMOCODE_AI_PROVIDER=ollama", "TERMOCODE_AI_MODEL=demo", "TERMOCODE_AI_BASE_URL=" + hm.srv.URL + "/v1"},
	}
	watchLeaks(t, r.home, r.ws)
	t.Cleanup(func() {
		if _, err := os.Stat(r.socket()); err == nil {
			_ = r.run("kill")
		}
		_ = os.RemoveAll(rt)
	})
	// The demo has finished the first-run setup card.
	cfg := filepath.Join(r.home, ".config", "termocode")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"setup_done": true, "animations": "off"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := &heroRecorder{t0: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}
	go rec.run()

	// 0-4 s: termocode opens a project.
	rec.phase("One terminal. Your whole project.", 4*time.Second)
	tm := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env, Cols: 200, Rows: 50})
	rec.attach(tm)
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	r.waitSocket(true)
	sleep(700 * time.Millisecond)
	tm.Send("\x1ba") // the AI chat opens beside the editor
	tm.WaitText("AI CHAT", uiTimeout)
	tm.settle()
	sleep(900 * time.Millisecond)

	// 4-9 s: edit a file, then a query in the Databases panel.
	rec.phase("Edit, query, ship — on the server.", 5*time.Second)
	tm.OpenFile("main.go")
	tm.WaitText("listUsers", uiTimeout)
	sleep(300 * time.Millisecond)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Send(KeyEsc)
	tm.settle()
	tm.Send("O")
	sleep(150 * time.Millisecond)
	tm.typeSlow("// Serve the shop API: list users, newest first.")
	tm.Send(KeyEsc)
	tm.settle()
	tm.Palette("Databases: Focus")
	tm.WaitText("DATABASES", uiTimeout)
	tm.WaitText("app", uiTimeout)
	tm.Send("n") // a SQL console for the connection
	tm.WaitText("@conn app", 15*time.Second)
	tm.settle()
	tm.typeSlow("SELECT email, plan FROM users ORDER BY id DESC;")
	tm.Send(KeyEsc)
	tm.settle()
	tm.Palette("Databases: Run Statement or Selection")
	tm.WaitText("ada@example.com", 15*time.Second)
	tm.settle()
	sleep(600 * time.Millisecond)

	// 9-14 s: a task given to the AI starts working.
	rec.phase("AI works with your IDE.", 5*time.Second)
	tm.ClickText("Ask AI anything") // the chat input takes the keys
	tm.settle()
	tm.typeSlow("Why is this query slow, and how do I fix it?")
	tm.Send(KeyEnter)
	waitChan(t, hm.started, "the model's first request", 20*time.Second)
	tm.WaitText("I looked at the users table", uiTimeout)
	sleep(2200 * time.Millisecond)

	// 14-17 s: the connection is really dropped.
	rec.phase("Connection lost.", 3*time.Second)
	tm.hangUp()
	if _, err := os.Stat(r.socket()); err != nil {
		t.Fatalf("session gone after the client died: %v", err)
	}
	// The dead terminal shows its last screen, frozen; then it is reset
	// (what closing a dead window does), and the screen is dark.
	sleep(1100 * time.Millisecond)
	tm.mu.Lock()
	_, _ = tm.em.WriteString("\x1b[?1049l\x1b[2J\x1b[H")
	tm.mu.Unlock()
	sleep(600 * time.Millisecond)
	// The model finishes while nobody is attached.
	close(hm.release)
	waitChan(t, hm.served, "the model's answer (the run went on with no client)", 20*time.Second)
	sleep(700 * time.Millisecond)

	// 17-23 s: reconnect: the file, the query result and the answer are there.
	rec.phase("Your work lives on.", 6*time.Second)
	tm2 := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env, Cols: 200, Rows: 50})
	rec.attach(tm2)
	tm2.WaitText("ada@example.com", bootTimeout)
	tm2.WaitText("users_plan_idx", uiTimeout)
	tm2.settle()
	sleep(1800 * time.Millisecond)
	tm2.clickTab("main.go")
	tm2.WaitText("Serve the shop API", uiTimeout)
	sleep(1800 * time.Millisecond)
	rec.phase("", 0)

	close(rec.stop)
	<-rec.done
	writeHero(t, out, rec)
	tm2.quitDiscard()
	r.waitSocket(false)
}

// writeHero writes the frames. Each sample lasts until the next one. A
// scene that took longer than its time in the GIF is played a little
// faster; a shorter one holds its last frame. A scene with no change on
// screen (the dark screen after the disconnect) repeats the frame it
// started with.
func writeHero(t *testing.T, dir string, rec *heroRecorder) {
	t.Helper()
	end := rec.now()
	phases := rec.phases[:len(rec.phases)-1] // the last mark only ends the scene before it
	bounds := make([]time.Duration, 0, len(phases)+1)
	for _, p := range phases {
		bounds = append(bounds, p.at)
	}
	bounds = append(bounds, end)

	type shot struct {
		at   time.Duration
		ansi string
	}
	var shots []shot
	started := false
	for _, s := range rec.samples {
		if !started && strings.TrimSpace(ansiPlain(s.ansi)) == "" {
			continue // the PTY before the first screen
		}
		started = true
		shots = append(shots, shot{s.at, s.ansi})
	}
	if len(shots) == 0 {
		t.Fatal("no screens recorded")
	}
	// A shot at the start of every scene, so a quiet scene still has one.
	at := func(d time.Duration) string {
		cur := shots[0].ansi
		for _, s := range shots {
			if s.at <= d {
				cur = s.ansi
			}
		}
		return cur
	}
	var starts []shot
	for _, b := range bounds[:len(bounds)-1] {
		starts = append(starts, shot{b, at(b)})
	}
	shots = append(shots, starts...)
	sort := func() {
		for i := 1; i < len(shots); i++ {
			for j := i; j > 0 && shots[j].at < shots[j-1].at; j-- {
				shots[j], shots[j-1] = shots[j-1], shots[j]
			}
		}
	}
	sort()

	var frames, caps []map[string]any
	var total time.Duration
	n := 0
	emit := func(ansi string, hold time.Duration) {
		name := fmt.Sprintf("%04d.ans", n)
		n++
		if err := os.WriteFile(filepath.Join(dir, name), []byte(ansi), 0o644); err != nil {
			t.Fatal(err)
		}
		frames = append(frames, map[string]any{"file": name, "hold_ns": int64(hold)})
		total += hold
	}
	for pi, ph := range phases {
		lo, hi := bounds[pi], bounds[pi+1]
		real := hi - lo
		scale := 1.0
		if real > ph.target {
			scale = float64(ph.target) / float64(real)
		}
		caps = append(caps, map[string]any{"at_ns": int64(total), "text": ph.caption})
		start := total
		var lastAnsi string
		for i, s := range shots {
			if s.at < lo || s.at >= hi {
				continue
			}
			next := hi
			if i+1 < len(shots) && shots[i+1].at < hi {
				next = shots[i+1].at
			}
			hold := time.Duration(float64(next-s.at) * scale)
			emit(s.ansi, max(hold, 50*time.Millisecond))
			lastAnsi = s.ansi
		}
		// Hold the last screen to fill the scene's time.
		if short := ph.target - (total - start); short > 0 && lastAnsi != "" {
			frames[len(frames)-1]["hold_ns"] = frames[len(frames)-1]["hold_ns"].(int64) + int64(short)
			total += short
		}
	}
	b, _ := json.MarshalIndent(map[string]any{"name": "hero", "note": "demo models", "frames": frames, "captions": caps}, "", " ")
	if err := os.WriteFile(filepath.Join(dir, "frames.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("hero: %d screens, %.1f s", len(frames), total.Seconds())
}

// ansiPlain strips escape sequences.
func ansiPlain(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < '@' || s[i] > '~') {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
