package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/afteradd"
)

// afterAddFakeSSH answers the scan lines with canned output and records
// what ran; any line outside the allowlist fails the test.
func afterAddFakeSSH(t *testing.T, out map[string]string) *[]string {
	t.Helper()
	var mu sync.Mutex
	var ran []string
	old := afterAddSSHRun
	afterAddSSHRun = func(_ context.Context, host, line string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		ok := false
		for _, c := range afterAddSSHScan {
			if c.line == line {
				ok = true
				ran = append(ran, host+" "+c.name)
				if v, has := out[c.name]; has {
					return v, nil
				}
			}
		}
		if !ok {
			t.Errorf("a line outside the allowlist ran: %q", line)
		}
		return "", errors.New("exit status 1")
	}
	t.Cleanup(func() { afterAddSSHRun = old })
	return &ran
}

// Spec (owner 2026-11-15, after-add 4): a new ssh server asks once
// whether to scan it, and the card lists the exact read-only commands
// first; nothing runs before Scan. Scan runs only the allowlist over
// ssh and a second card says what it found, a database first: Add DB
// connection through an ssh tunnel. .env files give names, never values.
func TestAfterAddSSHScanFlow(t *testing.T) {
	m := agentMCPTestModel(t)
	ran := afterAddFakeSSH(t, map[string]string{
		"containers": "shop-db\tpostgres:16\t5432/tcp\nweb\tnginx:1\t80/tcp\n",
		"ports":      "LISTEN 0 4096 0.0.0.0:5432 0.0.0.0:*\nLISTEN 0 511 0.0.0.0:80 0.0.0.0:*\nLISTEN 0 511 [::]:80 [::]:*\n",
		"services":   "nginx.service loaded active running nginx\ndocker.service loaded active running Docker\ncron.service loaded active running cron\n",
		"apps":       "== /srv/shop/compose.yaml\n== /srv/shop/.env\nDATABASE_URL\nSECRET_KEY\n",
	})
	var ops []string
	afterAddRunOp = func(m *Model, op, arg string) tea.Cmd {
		ops = append(ops, op)
		if cmd, ok := m.afterAddRunInternal(op, arg); ok {
			return cmd
		}
		return nil
	}
	t.Cleanup(func() { afterAddRunOp = afterAddRunOpReal })

	if cmd := m.afterAddSSHAdded("web1", "unix:///var/run/docker.sock"); cmd != nil {
		t.Fatal("a local context asked for a scan")
	}
	m.onAfterAdd(m.afterAddSSHAdded("web1", "ssh://deploy@65.109.212.14")().(afterAddMsg))
	if !m.confirmOpen || m.confirmKind != confirmKindAfterAdd {
		t.Fatal("no scan question")
	}
	view := strings.Join(strings.Fields(m.confirm.View()), " ")
	for _, want := range []string{"Scan web1? (read-only)", "deploy@65.109.212.14", "docker ps", "ss -ltnH", "names only"} {
		if !strings.Contains(view, want) {
			t.Errorf("question misses %q:\n%s", want, view)
		}
	}
	checkCardRule(t, "scan question", *m, false)
	if len(*ran) != 0 {
		t.Fatalf("ran before the answer: %v", *ran)
	}

	m.confirmOpen = false
	cmd := m.handleConfirmSelect("do")
	if cmd == nil {
		t.Fatal("Scan started nothing")
	}
	var scan afterAddMsg
	for _, msg := range afterAddMsgs(cmd) {
		scan = msg
	}
	if len(*ran) != len(afterAddSSHScan) || !slices.Contains(*ran, "deploy@65.109.212.14 containers") {
		t.Fatalf("ran %v", *ran)
	}
	m.onAfterAdd(scan)
	if !m.confirmOpen {
		t.Fatal("no found card")
	}
	view = strings.Join(strings.Fields(m.confirm.View()), " ")
	for _, want := range []string{"Found postgres on web1", "postgres in container shop-db", "port 5432 is open (postgres)", "open ports: 80 5432",
		"3 services running: nginx, docker", "compose apps: /srv/shop/compose.yaml", "1 .env files with 2 names (values not read)", "Add DB connection (ssh tunnel)"} {
		if !strings.Contains(view, want) {
			t.Errorf("found card misses %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "DATABASE_URL") {
		t.Error("the card lists .env names one by one")
	}
	m.confirmOpen = false
	m.handleConfirmSelect("do")
	if ops[len(ops)-1] != "db-add-tunnel" {
		t.Fatalf("ops %v", ops)
	}
}

// afterAddMsgs runs cmd (a batch too) and returns its after-add results.
func afterAddMsgs(cmd tea.Cmd) []afterAddMsg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case afterAddMsg:
		return []afterAddMsg{msg}
	case tea.BatchMsg:
		var out []afterAddMsg
		for _, c := range msg {
			out = append(out, afterAddMsgs(c)...)
		}
		return out
	}
	return nil
}

// A scan that finds nothing says so in a quiet toast; an unsafe host is
// never scanned.
func TestAfterAddSSHNothingFound(t *testing.T) {
	m := agentMCPTestModel(t)
	afterAddFakeSSH(t, map[string]string{"services": "cron.service loaded active running cron\n"})
	m.afterAddSSHAdded("bare", "ssh://u@h")
	m.onAfterAdd(afterAddMsg{res: m.afterAddEngine().Run(context.Background(), afteradd.Event{Kind: afterAddKindSSHScan, Item: "bare", Facts: map[string]string{"host": "u@h"}})})
	if m.confirmOpen || !strings.Contains(toastText(*m), "nothing to add") {
		t.Fatalf("open %v toast %q", m.confirmOpen, toastText(*m))
	}
	for _, h := range []string{"ssh://-oProxyCommand=x", "ssh://u@h;rm", "tcp://h:2376"} {
		if afterAddSSHHost(h) != "" {
			t.Errorf("%s is scanned", h)
		}
	}
}
