package gifkit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/devtools/shotkit"
)

// SceneClip is a GIF made from a shotkit scene: the real app model in
// process on a demo copy of the repository.
type SceneClip struct {
	Name    string
	Note    string // e.g. "demo models"
	Caption string // the caption from the first frame; Step.Caption starts later ones
	Scene   shotkit.Scene
	// ConfigDir is the private config root, for scenes that show its path.
	ConfigDir string
	// Intro is how long the start-up frame shows; 0 leaves it out.
	Intro time.Duration
	// Prepare runs before the scene (a fake engine on PATH, say) and
	// returns its cleanup.
	Prepare func(tmp string) (func(), error)
}

// EnterDemoWorkspace copies the repository (tracked files only) to a temp
// folder named "termocode" with a small fixed git history, makes it the
// working directory and fixes the clock; cleanup undoes both.
func EnterDemoWorkspace() (cleanup func(), err error) {
	noop := func() {}
	repo, err := os.Getwd()
	if err != nil {
		return noop, err
	}
	tmp, err := os.MkdirTemp("", "termocode-demo-")
	if err != nil {
		return noop, err
	}
	var restore func()
	cleanup = func() {
		_ = os.Chdir(repo)
		_ = os.RemoveAll(tmp)
		if restore != nil {
			restore()
		}
	}
	dst, err := shotkit.DemoWorkspace(repo, tmp)
	if err == nil {
		err = os.Chdir(dst)
	}
	if err == nil {
		err = os.Setenv("PWD", dst)
	}
	if err != nil {
		cleanup()
		return noop, err
	}
	restore = clock.Set(func() time.Time { return shotkit.DemoNow })
	return cleanup, nil
}

// Record plays the scene at 200x50 and returns the clip. The caller is in
// the demo workspace (EnterDemoWorkspace).
func Record(sc SceneClip) (Clip, error) {
	clip := Clip{Name: sc.Name, Note: sc.Note}
	if sc.Prepare != nil {
		tmp, err := os.MkdirTemp("", "termocode-gif-")
		if err != nil {
			return clip, err
		}
		defer os.RemoveAll(tmp)
		undo, err := sc.Prepare(tmp)
		if err != nil {
			return clip, err
		}
		defer undo()
	}
	// Each scene gets its own state, data and cache dirs: hot-exit backups
	// and AI run state of one scene must not show up in the next.
	state, err := os.MkdirTemp("", "termocode-gif-state-")
	if err != nil {
		return clip, err
	}
	defer os.RemoveAll(state)
	for _, k := range []string{"XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		old, had := os.LookupEnv(k)
		os.Setenv(k, filepath.Join(state, strings.ToLower(k)))
		defer func() {
			if had {
				os.Setenv(k, old)
			} else {
				os.Unsetenv(k)
			}
		}()
	}
	var at time.Duration
	i, pending := 0, sc.Caption
	steps := sc.Scene.Steps
	_, err = shotkit.Run(sc.Scene, Cols, Rows, shotkit.Options{Settle: true, Quiet: true, ConfigDir: sc.ConfigDir,
		OnFrame: func(f string, hold time.Duration) {
			text := ""
			switch {
			case i == 0:
				hold = sc.Intro
			case i-1 < len(steps):
				text = steps[i-1].Caption
			}
			if os.Getenv("GIF_DEBUG") != "" {
				for _, l := range strings.Split(shotkit.StripANSI(f), "\n") {
					if strings.Contains(l, os.Getenv("GIF_DEBUG")) {
						fmt.Fprintf(os.Stderr, "frame %d: %s\n", i, strings.TrimRight(l, " "))
					}
				}
			}
			i++
			if hold <= 0 { // a setup step (or the left-out start-up frame)
				return
			}
			if text == "" {
				text, pending = pending, ""
			}
			if text != "" {
				clip.Captions = append(clip.Captions, Caption{At: at, Text: text})
			}
			clip.Frames = append(clip.Frames, Frame{ANSI: f, Hold: hold})
			at += hold
		}})
	if err != nil {
		return clip, fmt.Errorf("%s: %w", sc.Name, err)
	}
	return clip, nil
}

// pathFirst puts dir first on PATH and returns the undo.
func pathFirst(dir string) func() {
	old := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+old)
	return func() { os.Setenv("PATH", old) }
}

// FakeDocker installs a fake `docker` (the e2e one: it answers from the
// canned JSON of internal/containers/testdata, logs a few lines and
// restarts nothing) first on PATH. repo is the repository root.
func FakeDocker(repo, tmp string) (func(), error) {
	bin := filepath.Join(tmp, "bin")
	data := filepath.Join(tmp, "data")
	for _, d := range []string{bin, data} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	src := filepath.Join(repo, "internal", "containers", "testdata", "docker27")
	for _, n := range []string{"version.json", "images.json", "network-ls.json", "volume-ls.json", "context-ls.json"} {
		b, err := os.ReadFile(filepath.Join(src, n))
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(data, n), b, 0o644); err != nil {
			return nil, err
		}
	}
	if err := os.WriteFile(filepath.Join(data, "ps.json"), []byte(fakePS), 0o644); err != nil {
		return nil, err
	}
	script := strings.ReplaceAll(fakeDockerScript, "@DATA@", data)
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		return nil, err
	}
	undo := pathFirst(bin)
	return func() {
		if os.Getenv("GIF_DEBUG") != "" {
			b, _ := os.ReadFile(filepath.Join(data, "argv.log"))
			fmt.Fprintf(os.Stderr, "fake docker calls:\n%s", b)
		}
		undo()
	}, nil
}

const fakeDockerScript = `#!/bin/sh
D='@DATA@'
printf '%s\n' "$*" >> "$D/argv.log"
case "$1" in
  version) cat "$D/version.json" ;;
  ps) if [ -f "$D/restarted" ]; then sed '/a1b2c3d4e5f6/s/Up 2 hours/Up 3 seconds/' "$D/ps.json"; else cat "$D/ps.json"; fi ;;
  images) cat "$D/images.json" ;;
  network) cat "$D/network-ls.json" ;;
  volume) cat "$D/volume-ls.json" ;;
  context) cat "$D/context-ls.json" ;;
  restart) sleep 1.0; touch "$D/restarted" ;;
  logs)
    i=0
    for l in "listening on :80" "GET /healthz 200 1ms" "GET /api/users 200 12ms" "GET /api/orders 200 9ms" "worker: job 4821 done" "GET /healthz 200 1ms" "restart requested"; do
      printf '2026-10-18T09:%02d:%02dZ %s\n' 1 $((i*7)) "$l"; i=$((i+1)); sleep 0.15
    done
    exec sleep 600 ;;
  events)
    echo $$ > "$D/events.pid"
    touch "$D/events.log"
    exec tail -n 0 -f "$D/events.log"
    ;;
esac
exit 0
`

// fakePS is `docker ps` of the fake engine: sample containers of a shop.
const fakePS = `{"ID":"9f3c2e1a1234","Names":"shop-web-1","Image":"ghcr.io/acme/shop-web:1.4","State":"running","Status":"Up 2 hours (healthy)","Ports":"0.0.0.0:8080->8080/tcp","Labels":"com.docker.compose.project=shop,com.docker.compose.service=web","Networks":"shop_default"}
{"ID":"a1b2c3d4e5f6","Names":"shop-api-1","Image":"ghcr.io/acme/shop-api:1.4","State":"running","Status":"Up 2 hours","Ports":"0.0.0.0:3000->3000/tcp","Labels":"com.docker.compose.project=shop,com.docker.compose.service=api","Networks":"shop_default"}
{"ID":"b2c3d4e5f6a7","Names":"shop-db-1","Image":"postgres:16","State":"running","Status":"Up 2 hours (healthy)","Ports":"5432/tcp","Labels":"com.docker.compose.project=shop,com.docker.compose.service=db","Networks":"shop_default"}
{"ID":"c3d4e5f6a7b8","Names":"shop-cache-1","Image":"redis:7","State":"running","Status":"Up 2 hours","Ports":"6379/tcp","Labels":"com.docker.compose.project=shop,com.docker.compose.service=cache","Networks":"shop_default"}
{"ID":"d4e5f6a7b8c9","Names":"shop-worker-1","Image":"ghcr.io/acme/shop-worker:1.4","State":"exited","Status":"Exited (1) 3 minutes ago","Labels":"com.docker.compose.project=shop,com.docker.compose.service=worker","Networks":"shop_default"}
{"ID":"aabbccdd0011","Names":"nginx","Image":"nginx:1.27","State":"running","Status":"Up 3 days","Ports":"0.0.0.0:8081->80/tcp","Labels":""}
`

// FakeCommand puts a harmless script called name first on PATH, so a scene
// never runs the real tool (`gh auth token` would read the user's login).
func FakeCommand(tmp, name, script string) (func(), error) {
	bin := filepath.Join(tmp, "bin-"+name)
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		return nil, err
	}
	return pathFirst(bin), nil
}
