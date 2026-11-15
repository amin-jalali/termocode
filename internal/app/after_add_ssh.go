package app

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/afteradd"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// After add: an ssh host (after_add.go). When the user adds a server
// (+ Add Server… → an ssh:// context), one card asks once: scan this
// server? It lists the exact commands first; they are a fixed read-only
// list (afterAddSSHScan) and nothing else ever runs. Yes runs them over
// ssh (BatchMode: no password prompt) and a second card says what was
// found: databases (containers and listening ports 5432 3306 6379
// 27017), containers and compose apps, kube (k3s, kubeconfig), systemd
// services, open ports, and the NAMES in the apps' .env files (never a
// value). Its one action: add a DB connection through an ssh tunnel, or
// open Containers, or open Kubernetes.

// afterAddKindSSHScan is the event of the scan itself (the user said yes).
const afterAddKindSSHScan = "ssh-scan"

// afterAddOpSSHScan is the first card's action: run the scan.
const afterAddOpSSHScan = "afteradd:ssh-scan"

// afterAddSSHCmd is one command of the scan: a fixed remote shell line.
type afterAddSSHCmd struct{ name, line string }

// afterAddSSHScan is the whole allowlist. Each line only lists or reads;
// none takes any input.
var afterAddSSHScan = []afterAddSSHCmd{
	{"containers", `docker ps --format '{{.Names}}\t{{.Image}}\t{{.Ports}}'`},
	{"ports", `ss -ltnH`},
	{"services", `systemctl list-units --type=service --state=running --no-legend --plain`},
	{"kube", `ls /etc/rancher/k3s/k3s.yaml ~/.kube/config 2>/dev/null; command -v kubectl k3s`},
	{"apps", `for f in $(find /srv /opt ~ -maxdepth 3 \( -name 'compose.y*ml' -o -name 'docker-compose.y*ml' -o -name .env \) 2>/dev/null); do echo "== $f"; case "$f" in *.env) sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' "$f";; esac; done`},
}

// afterAddSSHRun runs one scan line on host (tests replace it).
var afterAddSSHRun = func(ctx context.Context, host, line string) (string, error) {
	cmd := spawn.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "--", host, line)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if e := strings.TrimSpace(errb.String()); e != "" {
			return out.String(), fmt.Errorf("%s", errLine(e))
		}
		return out.String(), err
	}
	return out.String(), nil
}

// afterAddSSHHost is the ssh target of a context host ("ssh://u@h:22"):
// "u@h" (a port goes as -p … is not supported: the scan uses ssh's own
// config for it). "" when it is not ssh or not safe as an argument.
func afterAddSSHHost(h string) string {
	u, err := url.Parse(h)
	if err != nil || u.Scheme != "ssh" || u.Hostname() == "" {
		return ""
	}
	t := u.Hostname()
	if u.User != nil && u.User.Username() != "" {
		t = u.User.Username() + "@" + t
	}
	if strings.HasPrefix(t, "-") || strings.ContainsAny(t, " \t\n;|&$`'\"\\") {
		return ""
	}
	return t
}

// afterAddSSHAdded asks once whether to scan the new server.
func (m *Model) afterAddSSHAdded(name, host string) tea.Cmd {
	target := afterAddSSHHost(host)
	if target == "" {
		return nil
	}
	eng := m.afterAddEngine()
	eng.Add(afteradd.Rule{Kind: afteradd.KindSSH, Suggest: func(ev afteradd.Event, _ []afteradd.Finding) *afteradd.Suggestion {
		fs := []afteradd.Finding{{Level: afteradd.Info, Text: "Read-only. It runs only these, over ssh as " + target + ":"}}
		for _, c := range afterAddSSHScan {
			fs = append(fs, afteradd.Finding{Level: afteradd.Info, Text: c.line})
		}
		fs = append(fs, afteradd.Finding{Level: afteradd.Info, Text: "Of .env files it reads the names only, never a value."})
		return &afteradd.Suggestion{Title: "Scan " + ev.Item + "? (read-only)", Findings: fs,
			Action: afteradd.Action{ID: "scan", Label: "Scan", Op: afterAddOpSSHScan, Arg: ev.Item + "\n" + target}}
	}})
	eng.Add(afterAddSSHScanRule())
	return m.afterAdd(afteradd.Event{Kind: afteradd.KindSSH, Item: name, Facts: map[string]string{"host": target}})
}

// afterAddRunInternal runs the engine's own ops (not palette ops).
func (m *Model) afterAddRunInternal(op, arg string) (tea.Cmd, bool) {
	if op != afterAddOpSSHScan {
		return nil, false
	}
	name, target, _ := strings.Cut(arg, "\n")
	return m.afterAdd(afteradd.Event{Kind: afterAddKindSSHScan, Item: name, Facts: map[string]string{"host": target}}), true
}

// afterAddSSHScanRule runs the allowlist and reads what it printed.
func afterAddSSHScanRule() afteradd.Rule {
	var checks []afteradd.Check
	for _, c := range afterAddSSHScan {
		checks = append(checks, afteradd.Check{Name: c.name, Timeout: 20 * time.Second, Run: func(ctx context.Context, ev afteradd.Event) ([]afteradd.Finding, error) {
			out, err := afterAddSSHRun(ctx, ev.Facts["host"], c.line)
			if err != nil && strings.TrimSpace(out) == "" {
				if c.name == "kube" { // nothing found: ls and command -v fail
					return nil, nil
				}
				return nil, err
			}
			return afterAddSSHRead(c.name, out), nil
		}})
	}
	return afteradd.Rule{Kind: afterAddKindSSHScan, Checks: checks, Suggest: afterAddSSHSuggest}
}

// afterAddDBImages are image names of database servers.
var afterAddDBImages = []string{"postgres", "mysql", "mariadb", "redis", "mongo"}

// afterAddDBPorts are the well-known database ports.
var afterAddDBPorts = map[int]string{5432: "postgres", 3306: "mysql", 6379: "redis", 27017: "mongo"}

// afterAddSSHRead turns one command's output into findings.
func afterAddSSHRead(name, out string) []afteradd.Finding {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if strings.TrimSpace(out) == "" {
		lines = nil
	}
	info := func(text string, facts ...string) afteradd.Finding {
		f := afteradd.Finding{Level: afteradd.Info, Text: text, Facts: map[string]string{}}
		for i := 0; i+1 < len(facts); i += 2 {
			f.Facts[facts[i]] = facts[i+1]
		}
		return f
	}
	var fs []afteradd.Finding
	switch name {
	case "containers":
		for _, l := range lines {
			parts := strings.Split(l, "\t")
			if len(parts) < 2 {
				continue
			}
			img := strings.ToLower(parts[1])
			for _, db := range afterAddDBImages {
				if strings.Contains(img, db) {
					fs = append(fs, info(db+" in container "+parts[0], "db", db))
				}
			}
		}
		if len(lines) > 0 {
			fs = append(fs, info(strconv.Itoa(len(lines))+" containers running", "containers", strconv.Itoa(len(lines))))
		}
	case "ports":
		var open []int
		for _, l := range lines {
			fields := strings.Fields(l)
			if len(fields) < 4 {
				continue
			}
			addr := fields[3]
			p, err := strconv.Atoi(addr[strings.LastIndex(addr, ":")+1:])
			if err == nil && !slices.Contains(open, p) {
				open = append(open, p)
			}
		}
		slices.Sort(open)
		for _, p := range open {
			if db := afterAddDBPorts[p]; db != "" {
				fs = append(fs, info(fmt.Sprintf("port %d is open (%s)", p, db), "db", db))
			}
		}
		if len(open) > 0 {
			var ps []string
			for _, p := range open {
				ps = append(ps, strconv.Itoa(p))
			}
			fs = append(fs, info("open ports: "+strings.Join(ps, " ")))
		}
	case "services":
		var known []string
		for _, l := range lines {
			unit := strings.TrimSuffix(strings.Fields(l + " x")[0], ".service")
			for _, k := range []string{"nginx", "postgresql", "mysql", "mariadb", "redis", "docker", "k3s", "caddy", "apache2"} {
				if unit == k || strings.HasPrefix(unit, k+"-") || strings.HasPrefix(unit, k+"@") {
					known = append(known, unit)
				}
			}
		}
		if len(lines) > 0 {
			t := strconv.Itoa(len(lines)) + " services running"
			if len(known) > 0 {
				t += ": " + strings.Join(known, ", ")
			}
			fs = append(fs, info(t))
		}
	case "kube":
		for _, l := range lines {
			switch {
			case strings.HasSuffix(l, "k3s.yaml") || strings.HasSuffix(l, "/k3s"):
				fs = append(fs, info("k3s is installed", "kube", "k3s"))
			case strings.HasSuffix(l, ".kube/config"):
				fs = append(fs, info("a kubeconfig is there", "kube", "config"))
			}
		}
		fs = slices.CompactFunc(fs, func(a, b afteradd.Finding) bool { return a.Text == b.Text })
	case "apps":
		var compose, envs []string
		names := 0
		for _, l := range lines {
			if f, ok := strings.CutPrefix(l, "== "); ok {
				if strings.HasSuffix(f, ".env") {
					envs = append(envs, f)
				} else {
					compose = append(compose, f)
				}
				continue
			}
			names++
		}
		if len(compose) > 0 {
			fs = append(fs, info("compose apps: "+strings.Join(compose, ", "), "containers", strconv.Itoa(len(compose))))
		}
		if len(envs) > 0 {
			fs = append(fs, info(fmt.Sprintf("%d .env files with %d names (values not read)", len(envs), names)))
		}
	}
	return fs
}

// afterAddSSHSuggest: a database first, then containers, then kube.
func afterAddSSHSuggest(ev afteradd.Event, fs []afteradd.Finding) *afteradd.Suggestion {
	var db, kube string
	containers := false
	for _, f := range fs {
		if v := f.Facts["db"]; v != "" && db == "" {
			db = v
		}
		if v := f.Facts["kube"]; v != "" {
			kube = v
		}
		if f.Facts["containers"] != "" {
			containers = true
		}
	}
	switch {
	case db != "":
		return &afteradd.Suggestion{Title: "Found " + db + " on " + ev.Item,
			Action: afteradd.Action{ID: "db", Label: "Add DB connection (ssh tunnel)", Op: "db-add-tunnel"}}
	case containers:
		return &afteradd.Suggestion{Title: "Found containers on " + ev.Item,
			Action: afteradd.Action{ID: "containers", Label: "Open Containers", Op: "containers-focus"}}
	case kube != "":
		return &afteradd.Suggestion{Title: "Found Kubernetes on " + ev.Item,
			Action: afteradd.Action{ID: "kube", Label: "Open Kubernetes", Op: "kubernetes-show"}}
	}
	return nil
}
