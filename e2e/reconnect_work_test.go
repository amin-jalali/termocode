//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// SCN-27: the ssh link drops while three kinds of work run on the far
// side: a container log tab that follows `docker logs -f`, a slow SQL
// query in a console, and a kitty download. No real host, engine or
// network: a fake ssh (reconnect_test.go), a fake docker in the same
// folder and the real sqlite3 CLI (skipped when it is missing).

// fakeDockerLogsScript is fakeDockerScript plus `logs`: it prints the
// log file and follows it, as `docker logs -f` does.
const fakeDockerLogsScript = `#!/bin/sh
D='@DATA@'
printf '%s\n' "$*" >> "$D/argv.log"
case "$1" in
  version) cat "$D/version.json" ;;
  ps) cat "$D/ps.json" ;;
  images) cat "$D/images.json" ;;
  network) cat "$D/network-ls.json" ;;
  volume) cat "$D/volume-ls.json" ;;
  context) cat "$D/context-ls.json" ;;
  events)
    touch "$D/events.log"
    exec tail -n 0 -f "$D/events.log"
    ;;
  logs)
    echo $$ > "$D/logs.pid"
    exec tail -n +1 -f "$D/app.log"
    ;;
esac
exit 0
`

// scn27Query counts to 40 million: some seconds of work in sqlite3, so
// the link can drop while it runs.
const scn27Query = "WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c WHERE x < 40000000) SELECT count(*) + 7 AS scn27_rows FROM c;"

// installFakeDockerLogs puts the fake docker into the fake ssh's folder
// (first on PATH for both termocode processes) and returns its data dir.
func installFakeDockerLogs(t *testing.T, binDir string) string {
	t.Helper()
	data := t.TempDir()
	src := filepath.Join("..", "internal", "containers", "testdata", "docker27")
	for _, n := range []string{"version.json", "ps.json", "images.json", "network-ls.json", "volume-ls.json", "context-ls.json"} {
		b, err := os.ReadFile(filepath.Join(src, n))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(data, n), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(data, "app.log"), []byte("scn27-boot-line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(fakeDockerLogsScript, "@DATA@", data)
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if pidAlive(filepath.Join(data, "logs.pid")) {
			pid := strings.TrimSpace(fileText(filepath.Join(data, "logs.pid")))
			_ = exec.Command("kill", pid).Run()
		}
	})
	return data
}

func appendLog(t *testing.T, data, line string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(data, "app.log"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}

// selectNginx walks the Containers view down to the nginx row.
func selectNginx(t *testing.T, tm *Term) {
	t.Helper()
	tm.Palette("Containers: Focus")
	tm.WaitText("CONTAINERS", uiTimeout)
	tm.WaitText("nginx", uiTimeout)
	// The footer names the row's image a moment after the cursor lands
	// (slower on macOS with a query running): on the nginx row wait for
	// it, rather than pressing Down past the row.
	onNginx := func() bool {
		for _, l := range tm.Lines() {
			if strings.Contains(l, "▌") && strings.Contains(l, "nginx") && !strings.Contains(l, "logs:") {
				return true
			}
		}
		return false
	}
	for i := 0; i < 30 && !cursorFooterHas(tm, "nginx:1.27"); i++ {
		if onNginx() {
			tm.WaitFor("the nginx row's footer", uiTimeout, func([]string) bool { return cursorFooterHas(tm, "nginx:1.27") })
			break
		}
		tm.Send(KeyDown)
		tm.settle()
	}
	if !cursorFooterHas(tm, "nginx:1.27") {
		t.Fatalf("could not reach the nginx row:\n%s", tm.Screen())
	}
}

// startStalledKittyDownload opens big.txt and starts a kitty download
// that kitty allows and then never finishes: the far side waits for the
// next answer when the link drops.
func startStalledKittyDownload(t *testing.T, tm *Term) {
	t.Helper()
	tm.OpenFile("big.txt")
	tm.WaitText("line 0:", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Palette("File: Download to This Computer")
	tm.WaitText("Downloading big.txt", uiTimeout)
	// Allow it and say the file started, as kitty would; nothing more.
	seen, answered := 0, map[string]bool{}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !answered["file"] {
		all := kittyCmdRe.FindAllStringSubmatch(tm.rawOutput(), -1)
		for _, m := range all[seen:] {
			c := kv(m[1])
			switch c["ac"] {
			case "send":
				if !answered["send"] {
					tm.Send("\x1b]5113;ac=status;id=" + c["id"] + ";st=" + b64("OK") + "\x1b\\")
					answered["send"] = true
				}
			case "file":
				if !answered["file"] {
					tm.Send("\x1b]5113;ac=status;id=" + c["id"] + ";fid=" + c["fid"] + ";st=" + b64("STARTED") + "\x1b\\")
					answered["file"] = true
				}
			}
		}
		seen = len(all)
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSSHReconnectKeepsLogsQueryAndStopsDownload(t *testing.T) {
	sqlite := requireSQLite(t)
	r := newSSHRig(t)
	r.env = append(r.env, "TERMOCODE_TRANSFER=kitty")
	data := installFakeDockerLogs(t, r.dir)
	bigFile(t, r.ws)
	if err := os.MkdirAll(filepath.Join(r.ws, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.ws, ".termocode", "databases.json"),
		[]byte(`{"connections":[{"id":"app","name":"app","driver":"sqlite","path":"app.db"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	seed := exec.Command(sqlite, filepath.Join(r.ws, "app.db"),
		"CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL);"+
			"INSERT INTO users (email) VALUES ('ada@example.com');")
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}
	tm := r.start()

	// 1. A log tab that follows the nginx container.
	selectNginx(t, tm)
	tm.Palette("Containers: Follow Logs")
	tm.WaitText("scn27-boot-line", 10*time.Second)
	waitUntil(t, "the docker logs process", 10*time.Second, func() bool { return pidAlive(filepath.Join(data, "logs.pid")) })

	// 2. A kitty download that is still running.
	startStalledKittyDownload(t, tm)

	// 3. A slow query in a SQL console.
	tm.Palette("Databases: Focus")
	tm.WaitText("DATABASES", uiTimeout)
	tm.WaitText("app", uiTimeout)
	tm.Send("n")
	tm.WaitText("@conn app", 15*time.Second)
	tm.settle()
	tm.Type(scn27Query)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Palette("Databases: Run Statement or Selection")

	// The link drops while all three run, and stays down for a moment.
	r.set("fail", true)
	if err := syscall.Kill(r.pid("ssh.pid"), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	tm.WaitText("Reconnecting", uiTimeout)
	appendLog(t, data, "scn27-offline-line")
	tm.WaitFor("a retry", uiTimeout, func([]string) bool { return r.runs() >= 3 })
	r.set("fail", false)
	tm.WaitText("Reconnected after", 20*time.Second)

	// The download to the old terminal is stopped and says why; no
	// kitty answer text reaches the screen.
	tm.WaitText("Download stopped", uiTimeout)
	if tm.Has("is this really kitty") {
		t.Errorf("the download ended in a kitty timeout:\n%s", tm.Screen())
	}
	for _, junk := range []string{"5113", "ac=status"} {
		if tm.Has(junk) {
			t.Errorf("kitty text %q on the screen:\n%s", junk, tm.Screen())
		}
	}

	// The query ran on across the drop: its result is here.
	tm.WaitText("40000007", 60*time.Second) // not in the query text
	tm.WaitText("scn27_rows", uiTimeout)

	// The log tab is the same one (no second `docker logs`), it kept the
	// lines written while offline, and it still follows.
	if !pidAlive(filepath.Join(data, "logs.pid")) {
		t.Fatalf("the docker logs process ended with the link")
	}
	selectNginx(t, tm)
	tm.Palette("Containers: Follow Logs")
	tm.WaitText("scn27-offline-line", uiTimeout)
	appendLog(t, data, "scn27-after-line")
	tm.WaitText("scn27-after-line", 10*time.Second)
	if n := strings.Count(fileText(filepath.Join(data, "argv.log")), "logs -f"); n != 1 {
		t.Errorf("%d `docker logs -f` runs, want the first one only:\n%s", n, fileText(filepath.Join(data, "argv.log")))
	}
	r.end(tm)
}
