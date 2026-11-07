package containers

import (
	"reflect"
	"testing"
)

func TestExecCommandOptions(t *testing.T) {
	e := &Engine{Binary: "/usr/bin/docker", Kind: "docker"}
	cases := []struct {
		name  string
		opts  ExecOptions
		label string
		argv  []string
	}{
		{"default is the bash-or-sh shell", ExecOptions{}, "shell: abc",
			[]string{"/usr/bin/docker", "exec", "-it", "abc", "sh", "-c", "command -v bash >/dev/null && exec bash || exec sh"}},
		{"picked shell", ExecOptions{Shell: "/bin/zsh"}, "exec: abc /bin/zsh",
			[]string{"/usr/bin/docker", "exec", "-it", "abc", "/bin/zsh"}},
		{"user and workdir", ExecOptions{Shell: "bash", User: "root", Workdir: "/srv/app"}, "exec: abc bash",
			[]string{"/usr/bin/docker", "exec", "-it", "--user", "root", "--workdir", "/srv/app", "abc", "bash"}},
		{"command wins over shell and is passed as argv (no shell)", ExecOptions{Shell: "bash", Command: []string{"ls", "-la", "a b"}}, "exec: abc ls -la a b",
			[]string{"/usr/bin/docker", "exec", "-it", "abc", "ls", "-la", "a b"}},
		{"uid:gid user", ExecOptions{Command: []string{"id"}, User: "1000:1000"}, "exec: abc id",
			[]string{"/usr/bin/docker", "exec", "-it", "--user", "1000:1000", "abc", "id"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd, err := e.ExecCommand("abc", c.opts)
			if err != nil {
				t.Fatal(err)
			}
			if cmd.Label != c.label || !reflect.DeepEqual(cmd.Argv, c.argv) {
				t.Fatalf("got %q %q, want %q %q", cmd.Label, cmd.Argv, c.label, c.argv)
			}
		})
	}
}

func TestExecCommandRejectsOptionInjection(t *testing.T) {
	e := &Engine{Binary: "docker", Kind: "docker"}
	bad := []struct {
		id   string
		opts ExecOptions
	}{
		{"-privileged", ExecOptions{}},
		{"", ExecOptions{}},
		{"abc", ExecOptions{User: "--privileged"}},
		{"abc", ExecOptions{Workdir: "-e"}},
		{"abc", ExecOptions{Workdir: "relative/dir"}},
		{"abc", ExecOptions{Shell: "bash -c x"}},
		{"abc", ExecOptions{Shell: "-c"}},
		{"abc", ExecOptions{User: "ro ot"}},
		{"abc", ExecOptions{Workdir: "/a\nb"}},
		{"abc", ExecOptions{Command: []string{""}}},
		{"abc", ExecOptions{Command: []string{"-x"}}},
	}
	for _, b := range bad {
		if cmd, err := e.ExecCommand(b.id, b.opts); err == nil {
			t.Errorf("ExecCommand(%q, %+v) = %q, want error", b.id, b.opts, cmd.Argv)
		}
	}
}
