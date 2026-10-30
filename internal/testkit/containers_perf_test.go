package testkit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/perfbudget"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// PERF-5: the Containers view on a big host — 100 containers in 20
// compose projects, every third one running with live stats — against
// the fake engine (never a real docker).

// bigHostFake installs the fake engine with the big host.
func bigHostFake(t *testing.T) *servicestest.Fake {
	t.Helper()
	wd, _ := os.Getwd()
	fixtures := filepath.Join(wd, "..", "containers", "testdata", "docker27")
	f := servicestest.New(t)
	for _, name := range []string{"version.json", "network-ls.json", "volume-ls.json", "context-ls.json"} {
		b, err := os.ReadFile(filepath.Join(fixtures, name))
		if err != nil {
			t.Fatal(err)
		}
		f.Set(t, name, string(b))
	}
	var ps, images, stats strings.Builder
	for i := 0; i < 100; i++ {
		proj, svc := fmt.Sprintf("proj%02d", i/5), fmt.Sprintf("svc%d", i%5)
		state, status := "exited", "Exited (0) 2 hours ago"
		if i%3 == 0 {
			state, status = "running", "Up 2 hours"
		}
		id := fmt.Sprintf("%064x", i+1)
		fmt.Fprintf(&ps, `{"ID":"%s","Names":"%s-%s-1","Image":"registry.example.com/team/app%d:1.%d","ImageID":"sha256:%064x","Command":"\"./run\"","CreatedAt":"2026-10-18 09:30:00 +0000 UTC","State":"%s","Status":"%s","Ports":"0.0.0.0:%d->80/tcp","Labels":"com.docker.compose.project=%s,com.docker.compose.service=%s,com.docker.compose.container-number=1,com.docker.compose.project.working_dir=/work/%s","Size":"1.2MB","Networks":"%s_default"}`+"\n",
			id, proj, svc, i%25, i%4, 1000+i%25, state, status, 8000+i, proj, svc, proj, proj)
		if state == "running" {
			fmt.Fprintf(&stats, `{"Container":"%s","ID":"%s","Name":"%s-%s-1","CPUPerc":"%d.5%%","MemUsage":"48.2MiB / 1GiB","MemPerc":"4.7%%","NetIO":"1.2kB / 3.4kB","BlockIO":"0B / 8.19kB","PIDs":"3"}`+"\n",
				id[:12], id[:12], proj, svc, i%40)
		}
	}
	for i := 0; i < 25; i++ {
		fmt.Fprintf(&images, `{"ID":"sha256:%064x","Repository":"registry.example.com/team/app%d","Tag":"1.%d","CreatedAt":"2026-10-18 09:30:00 +0000 UTC","CreatedSince":"2 weeks ago","Size":"421MB","Containers":"N/A"}`+"\n", 1000+i, i, i%4)
	}
	f.Set(t, "ps.json", ps.String())
	f.Set(t, "images.json", images.String())
	f.Set(t, "stats.json", stats.String())
	t.Setenv("PATH", f.PathEnv())
	t.Setenv("DOCKER_CONTEXT", "")
	return f
}

// Opening the Containers view on the big host: from the command to the
// first container rows on screen (engine found, 100 containers listed).
// The median of 5 fresh starts (one start is one sample: noisy).
func TestPerfContainersOpen100(t *testing.T) {
	perfbudget.Require(t)
	bigHostFake(t)
	var took []time.Duration
	for i := 0; i < 5; i++ {
		d := New(t, Options{W: 120, H: 40})
		d.Settle(200 * time.Millisecond)
		dt, ok := waitFrame(d, app.DevActionMsg{ID: "palette:containers-focus"}, func(s string) bool {
			return strings.Contains(s, "svc0-1") && strings.Contains(s, "proj00")
		}, 20*time.Second)
		if !ok {
			t.Fatalf("start %d: the containers did not show:\n%s", i, screen(d))
		}
		took = append(took, dt)
	}
	t.Logf("open: p50 %v, max %v", percentile(took, 50), percentile(took, 100))
	perfbudget.Check(t, "containers_open_100", ms(percentile(took, 50)))
}

// Moving the cursor through the big host's list: the median time from a
// key to the frame that shows the cursor on the next row. Live stats are
// on, so a running container under the cursor shows its charts.
func TestPerfContainersKeyFrame100(t *testing.T) {
	perfbudget.Require(t)
	f := bigHostFake(t)
	d := New(t, Options{W: 140, H: 50})
	d.Palette("containers-focus")
	if !d.WaitForText("svc0-1", 20*time.Second) || !d.WaitFor(func() bool { return f.Count("stats") > 0 }, 10*time.Second) {
		t.Fatalf("the containers or their stats did not load:\n%s", screen(d))
	}
	d.Settle(200 * time.Millisecond)
	d.Key("g")
	var took []time.Duration
	for i := 0; i < 40; i++ {
		before := cursorLine(screen(d))
		dt, ok := waitFrame(d, KeyMsg("j"), func(s string) bool { return cursorLine(s) != before }, 5*time.Second)
		if !ok {
			t.Fatalf("key %d did not move the cursor from %q:\n%s", i, before, screen(d))
		}
		took = append(took, dt)
	}
	t.Logf("key to frame: p50 %v, p95 %v, max %v", percentile(took, 50), percentile(took, 95), percentile(took, 100))
	perfbudget.Check(t, "containers_key_frame_100", ms(percentile(took, 50)))
}
