// Package kubetest has a fake `kubectl` for tests: a shell script in a
// temp dir that logs its argv and answers from files. No test ever talks
// to a real cluster or reads ~/.kube.
//
// Files in Fake.Data (set them with Set):
//
//	current-context.txt  answer of `config current-context` (missing: the
//	                     "current-context is not set" error)
//	contexts.txt         answer of `config get-contexts -o name`
//	namespace.txt        answer of `config view --minify …` (the context's
//	                     namespace)
//	<kind>.json          answer of `get <kind>` (pods, deployments,
//	                     services, jobs, namespaces, statefulsets, …)
//	<kind>.txt           answer of `get <kind>` when a template is used
//	                     (secrets: names and key names only)
//	<kind>.yaml          answer of `get <kind> <name> -o yaml`
//	<kind>.err           `get <kind>` fails with this stderr (exit 1)
//	<kind>.sleep         `get <kind>` sleeps this many seconds first
//	all.err              every call fails with this stderr (exit 1)
//	fail-<ctx>.err       every call with --context <ctx> fails with this
//	                     stderr (an unreachable cluster)
//	describe.txt         answer of `describe …`
//	api-resources.txt    answer of `api-resources …` (.err: it fails)
//	restart.err          `rollout restart` fails with this stderr
//	rollout-status.txt   answer of `rollout status …`
//	rollout-history.txt  answer of `rollout history …`
//	undo.err             `rollout undo` fails with this stderr
//	set-image.err        `set image` fails with this stderr
//	scale.err            `scale` fails with this stderr
//	delete.err           `delete` fails with this stderr
//	node.err             `cordon`, `uncordon` and `drain` fail with this
//	                     stderr
//	logs.txt             answer of `logs …`
//	exec.txt             answer of `exec …` (a pod directory listing)
//	exec.err             `exec …` fails with this stderr
//	cp.err               `cp …` fails with this stderr
//	kustomize.txt        answer of `kustomize <dir>` (.err: it fails)
//	pf-port.txt          the local port `port-forward` reports (43123)
//	pf.err               `port-forward` fails with this stderr
//	top-<what>.txt       answer of `top <what>` (pods, nodes)
//	top-<what>.err       `top <what>` fails with this stderr (exit 1)
//	diff.txt             answer of `diff -f …` (present: exit 1 = changes)
//	diff.err             `diff` fails with this stderr (exit 2)
//	apply.txt            answer of `apply -f …` (default: "applied")
//	apply.err            `apply` fails with this stderr (exit 1)
//	can-i.txt            answer of `auth can-i --list` (the table)
//	can-i.err            `auth can-i` fails with this stderr
//
// Written by the script: argv.log (one call per line), env.log
// (KUBECONFIG of each call, "|", then its argv), pf.pid (the pid of a
// running port-forward).
package kubetest

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Fake is one installed fake kubectl.
type Fake struct {
	Bin  string // dir with the `kubectl` script: put it first on PATH
	Data string // the answer files and logs
}

const script = `#!/bin/sh
D='@DATA@'
printf '%s\n' "$*" >> "$D/argv.log"
printf '%s|%s\n' "${KUBECONFIG-}" "$*" >> "$D/env.log"
ctx=""; prev=""; out=""
for a in "$@"; do
  if [ "$prev" = "--context" ]; then ctx="$a"; fi
  if [ "$prev" = "-o" ]; then out="$a"; fi
  case "$a" in --context=*) ctx="${a#--context=}" ;; esac
  prev="$a"; last="$a"
done
if [ -f "$D/all.err" ]; then cat "$D/all.err" >&2; exit 1; fi
if [ -n "$ctx" ] && [ "$1" != "config" ] && [ -f "$D/fail-$ctx.err" ]; then cat "$D/fail-$ctx.err" >&2; exit 1; fi
case "$1" in
config)
  case "$2" in
  current-context)
    if [ -f "$D/current-context.txt" ]; then cat "$D/current-context.txt"; exit 0; fi
    echo "error: current-context is not set" >&2; exit 1 ;;
  get-contexts) cat "$D/contexts.txt" 2>/dev/null; exit 0 ;;
  view) cat "$D/namespace.txt" 2>/dev/null; exit 0 ;;
  esac
  exit 0 ;;
get)
  kind="$2"
  if [ -f "$D/$kind.sleep" ]; then sleep "$(cat "$D/$kind.sleep")"; fi
  if [ -f "$D/$kind.err" ]; then cat "$D/$kind.err" >&2; exit 1; fi
  if [ "$out" = "yaml" ]; then cat "$D/$kind.yaml" 2>/dev/null; exit 0; fi
  if [ -f "$D/$kind.txt" ]; then cat "$D/$kind.txt"; exit 0; fi
  cat "$D/$kind.json" 2>/dev/null; exit 0 ;;
describe) cat "$D/describe.txt" 2>/dev/null; exit 0 ;;
api-resources)
  if [ -f "$D/api-resources.err" ]; then cat "$D/api-resources.err" >&2; exit 1; fi
  cat "$D/api-resources.txt" 2>/dev/null; exit 0 ;;
rollout)
  case "$2" in
  status) cat "$D/rollout-status.txt" 2>/dev/null; exit 0 ;;
  history) cat "$D/rollout-history.txt" 2>/dev/null; exit 0 ;;
  undo)
    if [ -f "$D/undo.err" ]; then cat "$D/undo.err" >&2; exit 1; fi
    echo "$3 rolled back"; exit 0 ;;
  esac
  if [ -f "$D/restart.err" ]; then cat "$D/restart.err" >&2; exit 1; fi
  echo "deployment.apps/$3 restarted"; exit 0 ;;
set)
  if [ -f "$D/set-image.err" ]; then cat "$D/set-image.err" >&2; exit 1; fi
  echo "$3 image updated"; exit 0 ;;
logs) cat "$D/logs.txt" 2>/dev/null; exit 0 ;;
top)
  if [ -f "$D/top-$2.err" ]; then cat "$D/top-$2.err" >&2; exit 1; fi
  cat "$D/top-$2.txt" 2>/dev/null; exit 0 ;;
scale)
  if [ -f "$D/scale.err" ]; then cat "$D/scale.err" >&2; exit 1; fi
  echo "$2 scaled"; exit 0 ;;
delete)
  if [ -f "$D/delete.err" ]; then cat "$D/delete.err" >&2; exit 1; fi
  echo "$2 \"$3\" deleted"; exit 0 ;;
diff)
  if [ -f "$D/diff.err" ]; then cat "$D/diff.err" >&2; exit 2; fi
  if [ -f "$D/diff.txt" ]; then cat "$D/diff.txt"; exit 1; fi
  exit 0 ;;
apply)
  if [ -f "$D/apply.err" ]; then cat "$D/apply.err" >&2; exit 1; fi
  if [ -f "$D/apply.txt" ]; then cat "$D/apply.txt"; else echo applied; fi
  exit 0 ;;
auth)
  if [ -f "$D/can-i.err" ]; then cat "$D/can-i.err" >&2; exit 1; fi
  cat "$D/can-i.txt" 2>/dev/null; exit 0 ;;
cordon|uncordon|drain)
  if [ -f "$D/node.err" ]; then cat "$D/node.err" >&2; exit 1; fi
  echo "node/$2 done"; exit 0 ;;
exec)
  if [ -f "$D/exec.err" ]; then cat "$D/exec.err" >&2; exit 1; fi
  cat "$D/exec.txt" 2>/dev/null; exit 0 ;;
kustomize)
  if [ -f "$D/kustomize.err" ]; then cat "$D/kustomize.err" >&2; exit 1; fi
  cat "$D/kustomize.txt" 2>/dev/null; exit 0 ;;
cp)
  if [ -f "$D/cp.err" ]; then cat "$D/cp.err" >&2; exit 1; fi
  exit 0 ;;
port-forward)
  if [ -f "$D/pf.err" ]; then cat "$D/pf.err" >&2; exit 1; fi
  echo $$ > "$D/pf.pid"
  port="$(cat "$D/pf-port.txt" 2>/dev/null || echo 43123)"
  echo "Forwarding from 127.0.0.1:$port -> ${last##*:}"
  echo "Forwarding from [::1]:$port -> ${last##*:}"
  exec tail -f /dev/null ;;
esac
exit 0
`

// New installs a fake kubectl in temp dirs.
func New(t testing.TB) *Fake {
	t.Helper()
	f := &Fake{Bin: t.TempDir(), Data: t.TempDir()}
	body := strings.ReplaceAll(script, "@DATA@", f.Data)
	if err := os.WriteFile(filepath.Join(f.Bin, "kubectl"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

// Kubectl is the script's path.
func (f *Fake) Kubectl() string { return filepath.Join(f.Bin, "kubectl") }

// PathEnv is a PATH value with the fake first.
func (f *Fake) PathEnv() string { return f.Bin + string(os.PathListSeparator) + os.Getenv("PATH") }

// Set writes one answer file.
func (f *Fake) Set(t testing.TB, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.Data, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Remove deletes one answer file.
func (f *Fake) Remove(name string) { _ = os.Remove(filepath.Join(f.Data, name)) }

// Calls returns the logged argv lines (without "kubectl").
func (f *Fake) Calls() []string { return lines(filepath.Join(f.Data, "argv.log")) }

// EnvCalls returns the env.log lines: "<KUBECONFIG>|<argv>".
func (f *Fake) EnvCalls() []string { return lines(filepath.Join(f.Data, "env.log")) }

// Count returns how many calls contain sub.
func (f *Fake) Count(sub string) int {
	n := 0
	for _, c := range f.Calls() {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

// PID reads a pid file the script wrote (pf.pid).
func (f *Fake) PID(name string) int {
	b, err := os.ReadFile(filepath.Join(f.Data, name))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// Fixtures copies the JSON answers in dir (the kube package's testdata)
// into the fake and sets one context "dev" with namespace "shop".
func (f *Fake) Fixtures(t testing.TB, dir string) {
	t.Helper()
	for _, name := range []string{"pods.json", "deployments.json", "services.json", "jobs.json", "namespaces.json",
		"statefulsets.json", "daemonsets.json", "replicasets.json", "cronjobs.json", "configmaps.json", "secrets.txt",
		"ingresses.json", "persistentvolumeclaims.json", "nodes.json", "events.json"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		f.Set(t, name, string(b))
	}
	f.Set(t, "current-context.txt", "dev\n")
	f.Set(t, "contexts.txt", "dev\nprod\n")
	f.Set(t, "namespace.txt", "shop")
}

func lines(path string) []string {
	b, _ := os.ReadFile(path)
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
