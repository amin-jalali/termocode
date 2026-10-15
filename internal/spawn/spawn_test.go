package spawn

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestTrampoline(t *testing.T) {
	prefix := "/data/data/com.termux/files/usr/bin/"
	tests := []struct {
		path string
		argv []string
		want []string // nil: no trampoline
	}{
		{prefix + "nvim", []string{"nvim", "--embed"},
			[]string{"/system/bin/sh", "-c", `exec "$0" "$@"`, prefix + "nvim", "--embed"}},
		{prefix + "git", []string{"git"},
			[]string{"/system/bin/sh", "-c", `exec "$0" "$@"`, prefix + "git"}},
		// An argument with spaces and quotes reaches the program as is:
		// the script never parses it.
		{prefix + "git", []string{"git", "commit", "-m", `a "b" $c`},
			[]string{"/system/bin/sh", "-c", `exec "$0" "$@"`, prefix + "git", "commit", "-m", `a "b" $c`}},
		{"./tool", []string{"./tool"},
			[]string{"/system/bin/sh", "-c", `exec "$0" "$@"`, "./tool"}},
		{"/system/bin/toybox", []string{"toybox", "ls"}, nil},
		{"/apex/com.android.runtime/bin/linker64", []string{"linker64"}, nil},
		{"/system/bin/sh", []string{"sh", "-c", "x"}, nil},
		{"", []string{"x"}, nil},
	}
	for _, tc := range tests {
		path, args, ok := trampoline(tc.path, tc.argv)
		if tc.want == nil {
			if ok {
				t.Errorf("%s: trampolined to %v, want direct", tc.path, args)
			}
			continue
		}
		if !ok || path != "/system/bin/sh" || !reflect.DeepEqual(args, tc.want) {
			t.Errorf("%s: got %q %q %v, want %q", tc.path, path, args, ok, tc.want)
		}
	}
}

func TestTrampolineRunsTheProgram(t *testing.T) {
	// The script must hand every argument over unchanged. Run it with
	// this machine's sh in place of Android's.
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	printf, err := exec.LookPath("printf")
	if err != nil {
		t.Skip("no printf")
	}
	_, args, _ := trampoline(printf, []string{"printf", "[%s]", "a b", `"q"`, "$HOME", ""})
	out, err := exec.Command(sh, args[1:]...).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), `[a b]["q"][$HOME][]`; got != want {
		t.Fatalf("output %q, want %q", got, want)
	}
}

func TestRealExecutable(t *testing.T) {
	self := "/data/data/com.termux/files/usr/bin/termocode"
	if got := realExecutable("/apex/com.android.runtime/bin/linker64", self); got != self {
		t.Errorf("under the linker: %q, want %q", got, self)
	}
	if got := realExecutable("/system/bin/linker64", self); got != self {
		t.Errorf("under /system linker: %q, want %q", got, self)
	}
	// Started directly (F-Droid Termux): /proc/self/exe is right, even
	// when the env var came from a parent.
	if got := realExecutable(self, "/other/termocode"); got != self {
		t.Errorf("direct: %q, want %q", got, self)
	}
	if got := realExecutable("/apex/com.android.runtime/bin/linker64", ""); got != "/apex/com.android.runtime/bin/linker64" {
		t.Errorf("no env: %q", got)
	}
}

func TestNotAndroidLeavesCommandsAlone(t *testing.T) {
	if onAndroid {
		t.Skip("android")
	}
	cmd := Command("sh", "-c", "true")
	if cmd.Args[0] != "sh" || len(cmd.Args) != 3 {
		t.Fatalf("args changed: %q", cmd.Args)
	}
}

// On Play Store Termux, termux-exec starts a Go program through the
// system linker and the program sees its own path twice in os.Args
// (checked on the tablet: [tc, tc, --version]); flags were never seen.
func TestLinkerArgs(t *testing.T) {
	const self = "/data/data/com.termux/files/home/tc"
	cases := []struct {
		args         []string
		exe, selfExe string
		want         []string
	}{
		{[]string{self, self, "--version"}, "/apex/com.android.runtime/bin/linker64", self, []string{self, "--version"}},
		{[]string{self, self}, "/system/bin/linker64", self, []string{self}},
		// Not started through the linker: leave it.
		{[]string{self, self, "x"}, self, "", []string{self, self, "x"}},
		// Through the linker, but the first argument is not the program.
		{[]string{self, "--version"}, "/system/bin/linker64", self, []string{self, "--version"}},
	}
	for _, c := range cases {
		got := linkerArgs(c.args, c.exe, c.selfExe)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("linkerArgs(%q, %q) = %q, want %q", c.args, c.exe, got, c.want)
		}
	}
}
