package mcp

import (
	"errors"
	"strings"
	"testing"
)

func sbServer(t *testing.T, sandbox string) Server {
	t.Helper()
	raw := `{"command":"npx","args":["-y","pkg"],"env":{"TOKEN":"v"},"sandbox":` + sandbox + `}`
	s := parseEntry("srv", []byte(raw))
	if s.Problem != "" {
		t.Fatalf("problem: %s", s.Problem)
	}
	s.Env = map[string]string{"TOKEN": "sk-" + "test-secret"}
	return s
}

func fakeLook(have ...string) func(string) (string, error) {
	return func(n string) (string, error) {
		for _, h := range have {
			if h == n {
				return "/fake/" + n, nil
			}
		}
		return "", errors.New("not found")
	}
}

func sbEnv(goos string, have ...string) sandboxEnv {
	return sandboxEnv{look: fakeLook(have...), goos: goos, home: "/home/u", tmp: "/tmp"}
}

// Spec: no "sandbox" key (or false) leaves the entry as it is.
func TestSandboxOffLeavesServer(t *testing.T) {
	for _, raw := range []string{`{"command":"npx"}`, `{"command":"npx","sandbox":false}`} {
		s := parseEntry("srv", []byte(raw))
		got, note, err := applySandbox(s, []string{"/w"}, sbEnv("linux", "bwrap"))
		if err != nil || note != "" || got.Command != "npx" {
			t.Fatalf("%s: %+v %q %v", raw, got, note, err)
		}
	}
}

// Spec: a container runs `docker run -i --rm` with network off and the
// workspace read-only by default; limits become flags; env names go as
// `-e NAME` and the secret value never reaches argv.
func TestSandboxContainerArgs(t *testing.T) {
	s := sbServer(t, `{"kind":"container","image":"node:22","cpus":"1","memory":"512m"}`)
	got, note, err := applySandbox(s, []string{"/w"}, sbEnv("linux", "docker"))
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(append([]string{got.Command}, got.Args...), " ")
	want := "docker run -i --rm --init --network none --cpus 1 --memory 512m -v /w:/w:ro -w /w -e TOKEN node:22 npx -y pkg"
	if argv != want {
		t.Fatalf("argv\n got %s\nwant %s", argv, want)
	}
	if strings.Contains(argv, "secret") {
		t.Fatal("secret in argv")
	}
	if got.Env["TOKEN"] == "" {
		t.Fatal("env value must stay in the environment")
	}
	if note != "sandbox: container (docker, node:22) · network off · workspace read-only · 1 CPU · 512m" {
		t.Fatalf("note %q", note)
	}
}

// Spec: network on and a read-write workspace are opt-in; podman is used
// when docker is missing.
func TestSandboxContainerOptIns(t *testing.T) {
	s := sbServer(t, `{"image":"node:22","network":true,"workspace":"rw"}`)
	got, _, err := applySandbox(s, []string{"/w"}, sbEnv("linux", "podman"))
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(got.Args, " ")
	if got.Command != "podman" || strings.Contains(argv, "--network") || !strings.Contains(argv, "/w:/w:rw") {
		t.Fatalf("%s %s", got.Command, argv)
	}
}

// Spec: bwrap hides the home folder, binds the workspace read-only and
// unshares the network; limits are noted as not applied.
func TestSandboxBwrap(t *testing.T) {
	s := sbServer(t, `{"kind":"bwrap","memory":"1g"}`)
	got, note, err := applySandbox(s, []string{"/w"}, sbEnv("linux", "bwrap"))
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(got.Args, " ")
	for _, w := range []string{"--unshare-all", "--tmpfs /home/u", "--ro-bind /w /w", "--chdir /w", " npx -y pkg"} {
		if !strings.Contains(argv, w) {
			t.Fatalf("missing %q in %s", w, argv)
		}
	}
	if strings.Contains(argv, "--share-net") || got.Command != "/fake/bwrap" {
		t.Fatalf("%s %s", got.Command, argv)
	}
	if !strings.Contains(note, "not applied") || !strings.Contains(note, "network off") {
		t.Fatalf("note %q", note)
	}
}

// Spec: on macOS "true" picks sandbox-exec with a profile that denies the
// network and writes outside temp.
func TestSandboxExecAuto(t *testing.T) {
	s := sbServer(t, `true`)
	got, _, err := applySandbox(s, []string{"/w"}, sbEnv("darwin", "sandbox-exec"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != "/fake/sandbox-exec" || got.Args[0] != "-p" || !strings.Contains(got.Args[1], "(deny network*)") ||
		!strings.Contains(got.Args[1], "(deny file-write*)") || strings.Contains(got.Args[1], `"/w"`) || got.Args[2] != "npx" {
		t.Fatalf("%v", got.Args)
	}
}

// Spec: nothing installed → a clear error and no start (never a run
// without the sandbox).
func TestSandboxNoToolFails(t *testing.T) {
	s := sbServer(t, `true`)
	_, _, err := applySandbox(s, []string{"/w"}, sbEnv("linux"))
	if err == nil || !strings.Contains(err.Error(), "no sandbox tool found") || !strings.Contains(err.Error(), "not started") {
		t.Fatalf("err %v", err)
	}
	s = sbServer(t, `{"image":"node:22","kind":"container"}`)
	if _, _, err := applySandbox(s, nil, sbEnv("linux")); err == nil || !strings.Contains(err.Error(), "docker or podman not found") {
		t.Fatalf("err %v", err)
	}
}

// Spec: bad values are refused (no flag injection through cpus, memory
// or image).
func TestSandboxBadValues(t *testing.T) {
	for _, raw := range []string{
		`{"kind":"vm"}`, `{"image":"node","cpus":"--privileged"}`, `{"image":"node","memory":"1g --pid=host"}`,
		`{"image":"--privileged"}`, `{"kind":"container"}`, `{"workspace":"all"}`, `{"image":"x","runtime":"lxc"}`,
	} {
		s := parseEntry("srv", []byte(`{"command":"x","sandbox":`+raw+`}`))
		if _, err := s.SandboxSpec(); err == nil {
			t.Fatalf("%s: no error", raw)
		}
	}
	h := parseEntry("srv", []byte(`{"url":"https://x.example/mcp","sandbox":true}`))
	if _, err := h.SandboxSpec(); err == nil {
		t.Fatal("http server with sandbox: no error")
	}
}

// Spec: the status row tells what sandbox an entry asks for.
func TestStatusSandboxText(t *testing.T) {
	cases := map[string]string{
		`{"command":"x"}`:                "",
		`{"command":"x","sandbox":true}`: "sandbox: auto · network off · workspace read-only",
		`{"command":"x","sandbox":{"image":"node:22","kind":"container"}}`: "sandbox: container (node:22) · network off · workspace read-only",
		`{"command":"x","sandbox":{"kind":"bwrap","network":true}}`:        "sandbox: bwrap · network on · workspace read-only",
		`{"command":"x","sandbox":{"kind":"vm"}}`:                          `sandbox: unknown kind "vm" (use container, bwrap or sandbox-exec)`,
	}
	for raw, want := range cases {
		if got := sandboxText(parseEntry("srv", []byte(raw))); got != want {
			t.Errorf("%s:\n got %q\nwant %q", raw, got, want)
		}
	}
}
