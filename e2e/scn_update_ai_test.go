//go:build e2e

package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/update"
)

// holdModel is a scripted OpenAI-compatible model that holds its answer
// until release is closed, and counts the requests it got.
type holdModel struct {
	srv     *httptest.Server
	release chan struct{}
	asked   chan struct{} // closed on the first request
	once    sync.Once
	mu      sync.Mutex
	n       int
}

func newHoldModel(t *testing.T, answer string) *holdModel {
	t.Helper()
	hm := &holdModel{release: make(chan struct{}), asked: make(chan struct{})}
	hm.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		hm.mu.Lock()
		hm.n++
		hm.mu.Unlock()
		hm.once.Do(func() { close(hm.asked) })
		select {
		case <-hm.release:
		case <-r.Context().Done():
			return
		}
		out, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": answer}}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", out)
	}))
	t.Cleanup(hm.srv.Close)
	return hm
}

func (hm *holdModel) requests() int {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	return hm.n
}

// newUpdateServer is a release server (TLS, self-signed: the e2etest build
// trusts it) with v0.2.0 for this system. The new "binary" is script.
func newUpdateServer(t *testing.T, script []byte) *httptest.Server {
	t.Helper()
	const tag = "v0.2.0"
	dir := update.BinaryDir(tag, runtime.GOOS, runtime.GOARCH)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: dir + "/termocode", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(script)
	_ = tw.Close()
	_ = gz.Close()
	tarball := update.TarballName(tag, runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(buf.Bytes())
	sums := hex.EncodeToString(sum[:]) + "  " + tarball + "\n"
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": tag, "html_url": srv.URL + "/notes",
				"assets": []map[string]any{
					{"name": tarball, "size": buf.Len(), "browser_download_url": srv.URL + "/" + tarball},
					{"name": "checksums.txt", "size": len(sums), "browser_download_url": srv.URL + "/checksums.txt"},
				},
			})
		case "/" + tarball:
			_, _ = w.Write(buf.Bytes())
		case "/checksums.txt":
			_, _ = w.Write([]byte(sums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// updateRig builds a release-like binary (-tags e2etest, v0.1.0), a fake
// release server whose "new binary" says v0.2.0 and then runs the old one
// (so a restart into it still works), and the session rig for it.
func updateRig(t *testing.T) (r *sessionRig, exe string) {
	t.Helper()
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64":
	default:
		t.Skip("no release build for this system")
	}
	dir := t.TempDir()
	exe = filepath.Join(dir, "termocode")
	build := exec.Command("go", "build", "-tags", "e2etest", "-ldflags", "-X main.version=v0.1.0", "-o", exe, "./cmd/termocode")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	orig := exe + ".orig"
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orig, b, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n[ \"$1\" = \"--version\" ] && { echo \"termocode v0.2.0\"; exit 0; }\nexec " + orig + " \"$@\"\n"
	srv := newUpdateServer(t, []byte(script))
	r = newSessionRig(t)
	r.env = append(r.env, "TERMOCODE_E2E_UPDATE_URL="+srv.URL+"/latest")
	return r, exe
}

// startUpdateClient starts a client of the e2etest binary exe.
func startUpdateClient(r *sessionRig, exe string) *Term {
	r.t.Helper()
	tm := Start(r.t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env, Bin: exe})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	return tm
}

// Spec (SCN-32): an update is installed while an AI turn runs. Restart on
// the card waits for the turn (a toast says so), the turn ends with its
// answer, and then termocode restarts; the answer is still in the chat and
// the model was asked once.
func TestSCN32UpdateRestartWaitsForAIRun(t *testing.T) {
	const answer = "update-run-answer-32"
	model := newHoldModel(t, answer)
	r, exe := updateRig(t)
	r.env = append(r.env, "TERMOCODE_AI_PROVIDER=ollama", "TERMOCODE_AI_MODEL=fake", "TERMOCODE_AI_BASE_URL="+model.srv.URL+"/v1")
	tm := startUpdateClient(r, exe)
	r.waitSocket(true)

	tm.Send("\x1ba")
	tm.WaitText("AI CHAT", uiTimeout)
	tm.Type("write something long")
	tm.Send(KeyEnter)
	select {
	case <-model.asked:
	case <-time.After(20 * time.Second):
		t.Fatalf("the model was never asked\n%s", tm.Screen())
	}

	tm.Palette("termocode: Check for Updates")
	tm.WaitText("termocode v0.2.0 is out", 20*time.Second)
	tm.settle()
	for _, k := range []string{"\x1b[C", "\x1b[C", "\x1b[C"} { // Later · Release notes · Skip · Update
		tm.Send(k)
		time.Sleep(150 * time.Millisecond)
	}
	tm.Send(KeyEnter)
	tm.WaitText("Install termocode v0.2.0?", 40*time.Second)
	tm.settle()
	tm.Send("\x1b[C") // Cancel · Install
	time.Sleep(150 * time.Millisecond)
	tm.Send(KeyEnter)
	tm.WaitText("termocode v0.2.0 is installed", 20*time.Second)
	if !tm.Has("An AI turn is running") {
		t.Errorf("the restart card does not name the running turn:\n%s", tm.Screen())
	}
	tm.settle()
	tm.Send("\x1b[C") // Later · Restart
	time.Sleep(150 * time.Millisecond)
	tm.Send(KeyEnter)
	tm.WaitText("Restarts when the AI turn ends", 10*time.Second)
	if b, _ := os.ReadFile(exe); !strings.Contains(string(b), "termocode v0.2.0") {
		t.Errorf("the installed file was not replaced: %q", b)
	}

	// The turn ends: the answer arrives, then termocode restarts.
	close(model.release)
	tm.WaitText(answer, 20*time.Second)
	// The restart redraws the whole screen; the answer is still in the chat.
	tm.settle()
	time.Sleep(3 * time.Second)
	tm.settle()
	if !tm.Has(answer) || !tm.Has("write something long") {
		t.Errorf("the chat lost its run after the restart:\n%s", tm.Screen())
	}
	if n := model.requests(); n != 1 {
		t.Errorf("the model was asked %d times, want 1 (the run was repeated)", n)
	}
}

// Spec (SCN-32): the update card opened in a session stays while the
// client detaches: a new client attaches and still sees the card, and
// Later closes it.
func TestSCN32UpdateCardSurvivesDetach(t *testing.T) {
	r, exe := updateRig(t)
	tm := startUpdateClient(r, exe)
	r.waitSocket(true)
	tm.Palette("termocode: Check for Updates")
	tm.WaitText("termocode v0.2.0 is out", 20*time.Second)
	tm.settle()
	tm.Disconnect() // the terminal goes away: the session keeps the open card
	tm2 := startUpdateClient(r, exe)
	tm2.WaitText("termocode v0.2.0 is out", 20*time.Second)
	tm2.settle()
	tm2.Send(KeyEnter) // Later is the first button
	tm2.WaitGone("termocode v0.2.0 is out", 10*time.Second)
	tm2.Quit()
}
