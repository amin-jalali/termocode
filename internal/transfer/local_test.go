package transfer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// ── Local or remote ──

func TestIsRemote(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want bool
	}{
		{map[string]string{}, false},
		{map[string]string{"TERM": "xterm-256color", "VTE_VERSION": "7600"}, false},
		{map[string]string{"SSH_CONNECTION": "10.0.0.5 50000 10.0.0.9 22"}, true},
		{map[string]string{"SSH_CLIENT": "10.0.0.5 50000 22"}, true},
		{map[string]string{"SSH_TTY": "/dev/pts/3"}, true},
		// A container or WSL is local unless SSH says otherwise.
		{map[string]string{"container": "podman", "WSL_DISTRO_NAME": "Ubuntu"}, false},
		{map[string]string{"SSH_CONNECTION": "  "}, false},
	}
	for _, c := range cases {
		if got := IsRemote(envOf(c.env)); got != c.want {
			t.Errorf("%v: IsRemote = %v, want %v", c.env, got, c.want)
		}
	}
}

// Locally the files are already on the user's computer: whatever the
// terminal (GNOME Terminal, kitty, tmux), Download and Upload copy.
func TestDetectLocal(t *testing.T) {
	cases := []struct {
		env      map[string]string
		down, up Method
	}{
		{map[string]string{"TERM": "xterm-256color", "VTE_VERSION": "7600"}, Local, Local},
		{map[string]string{"TERM": "xterm-kitty"}, Local, Local},
		{map[string]string{"TERM": "tmux-256color", "TMUX": "/tmp/tmux-1/default,1,0"}, Local, Local},
		// Over SSH the terminal decides, as before.
		{map[string]string{"TERM": "xterm-256color", "SSH_CONNECTION": "1 2 3 4"}, None, None},
		{map[string]string{"TERM": "xterm-kitty", "SSH_TTY": "/dev/pts/1"}, Kitty, Kitty},
		// Forced methods win; "local" forces the copy even over SSH.
		{map[string]string{"TERM": "xterm-256color", EnvOverride: "kitty"}, Kitty, Kitty},
		{map[string]string{"TERM": "xterm-256color", EnvOverride: "off"}, None, None},
		{map[string]string{"SSH_TTY": "/dev/pts/1", EnvOverride: "local"}, Local, Local},
	}
	for _, c := range cases {
		s := Detect(envOf(c.env))
		if s.Download != c.down || s.Upload != c.up {
			t.Errorf("%v: got %+v, want down=%q up=%q", c.env, s, c.down, c.up)
		}
	}
	if s := Detect(envOf(map[string]string{"SSH_TTY": "x"})); !s.Remote {
		t.Error("Remote not set over SSH")
	}
}

// ── Downloads folder ──

func TestParseUserDirs(t *testing.T) {
	data := []byte(`# This file is written by xdg-user-dirs-update
XDG_DESKTOP_DIR="$HOME/Desktop"
XDG_DOWNLOAD_DIR="$HOME/Téléchargements"
XDG_MUSIC_DIR="/data/music"
`)
	if got := ParseUserDirs(data, "XDG_DOWNLOAD_DIR", "/home/u"); got != "/home/u/Téléchargements" {
		t.Errorf("download = %q", got)
	}
	if got := ParseUserDirs(data, "XDG_MUSIC_DIR", "/home/u"); got != "/data/music" {
		t.Errorf("music = %q", got)
	}
	if got := ParseUserDirs(data, "XDG_VIDEOS_DIR", "/home/u"); got != "" {
		t.Errorf("missing = %q", got)
	}
	// "$HOME" alone means the folder is turned off.
	if got := ParseUserDirs([]byte(`XDG_DOWNLOAD_DIR="$HOME/"`), "XDG_DOWNLOAD_DIR", "/home/u"); got != "" {
		t.Errorf("home = %q", got)
	}
	// Relative paths are not allowed by the spec.
	if got := ParseUserDirs([]byte(`XDG_DOWNLOAD_DIR="dl"`), "XDG_DOWNLOAD_DIR", "/home/u"); got != "" {
		t.Errorf("relative = %q", got)
	}
}

func TestDownloadsDir(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, "cfg")
	must(t, os.MkdirAll(cfg, 0o755))
	must(t, os.WriteFile(filepath.Join(cfg, "user-dirs.dirs"), []byte(`XDG_DOWNLOAD_DIR="$HOME/Dl"`+"\n"), 0o644))
	env := map[string]string{"XDG_CONFIG_HOME": cfg}
	none := func() string { return "" }

	// xdg-user-dir wins.
	if got := DownloadsDir(envOf(env), home, "linux", func() string { return "/x/Downloads\n" }); got != "/x/Downloads" {
		t.Errorf("xdg-user-dir: %q", got)
	}
	// xdg-user-dir prints $HOME when the folder is not set: ignored.
	if got := DownloadsDir(envOf(env), home, "linux", func() string { return home }); got != filepath.Join(home, "Dl") {
		t.Errorf("xdg-user-dir = home: %q", got)
	}
	// user-dirs.dirs.
	if got := DownloadsDir(envOf(env), home, "linux", none); got != filepath.Join(home, "Dl") {
		t.Errorf("user-dirs.dirs: %q", got)
	}
	// The environment variable comes before the file.
	env["XDG_DOWNLOAD_DIR"] = "/env/dl"
	if got := DownloadsDir(envOf(env), home, "linux", none); got != "/env/dl" {
		t.Errorf("env: %q", got)
	}
	// Nothing set: ~/Downloads.
	if got := DownloadsDir(envOf(map[string]string{"XDG_CONFIG_HOME": t.TempDir()}), home, "linux", none); got != filepath.Join(home, "Downloads") {
		t.Errorf("default: %q", got)
	}
	// macOS: always ~/Downloads.
	if got := DownloadsDir(envOf(env), home, "darwin", func() string { return "/nope" }); got != filepath.Join(home, "Downloads") {
		t.Errorf("darwin: %q", got)
	}
}

// ── Local copy ──

func TestCopyIntoNeverOverwrites(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	f := filepath.Join(src, "PKGBUILD.tmpl")
	must(t, os.WriteFile(f, []byte("new"), 0o640))
	must(t, os.WriteFile(filepath.Join(dst, "PKGBUILD.tmpl"), []byte("old"), 0o644))

	var got int64
	name, err := CopyInto(context.Background(), f, dst, func(n int64) { got = n })
	if err != nil || name != "PKGBUILD (1).tmpl" {
		t.Fatalf("name %q err %v", name, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "PKGBUILD.tmpl")); string(b) != "old" {
		t.Error("the old file was replaced")
	}
	if b, _ := os.ReadFile(filepath.Join(dst, name)); string(b) != "new" {
		t.Errorf("copy = %q", b)
	}
	if got != 3 {
		t.Errorf("progress = %d", got)
	}
	if fi, _ := os.Stat(filepath.Join(dst, name)); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v", fi.Mode())
	}

	// A folder is copied as is (no archive), also under a free name.
	dir := filepath.Join(src, "notes")
	must(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "sub", "a.md"), []byte("A"), 0o644))
	must(t, os.MkdirAll(filepath.Join(dst, "notes"), 0o755))
	name, err = CopyInto(context.Background(), dir, dst, nil)
	if err != nil || name != "notes (1)" {
		t.Fatalf("dir name %q err %v", name, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, name, "sub", "a.md")); string(b) != "A" {
		t.Errorf("dir copy = %q", b)
	}
}

func TestCopyIntoCanceled(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	f := filepath.Join(src, "big.bin")
	must(t, os.WriteFile(f, make([]byte, 1<<20), 0o644))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CopyInto(ctx, f, dst, nil); !errors.Is(err, ErrCanceled) {
		t.Fatalf("err = %v", err)
	}
	if ents, _ := os.ReadDir(dst); len(ents) != 0 {
		t.Errorf("left %d entries behind", len(ents))
	}
}

// ── Native file chooser ──

func TestPickChooser(t *testing.T) {
	have := func(bins ...string) func(string) (string, error) {
		return func(b string) (string, error) {
			for _, x := range bins {
				if x == b {
					return "/usr/bin/" + b, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	gui := map[string]string{"WAYLAND_DISPLAY": "wayland-0"}
	cases := []struct {
		goos string
		env  map[string]string
		bins []string
		want string
	}{
		{"linux", gui, []string{"zenity", "kdialog"}, "zenity"},
		{"linux", map[string]string{"DISPLAY": ":0", "XDG_CURRENT_DESKTOP": "KDE"}, []string{"zenity", "kdialog"}, "kdialog"},
		{"linux", gui, []string{"kdialog"}, "kdialog"},
		{"linux", gui, []string{"yad"}, "yad"},
		{"linux", gui, nil, ""},
		// No display (a text console): no window can open.
		{"linux", map[string]string{}, []string{"zenity"}, ""},
		{"darwin", map[string]string{}, []string{"osascript"}, "osascript"},
		{"windows", gui, []string{"zenity"}, ""},
	}
	for _, c := range cases {
		ch, ok := PickChooser(c.goos, envOf(c.env), have(c.bins...), "Upload to notes", "/home/u")
		if ok != (c.want != "") || (ok && ch.Name != c.want) {
			t.Errorf("%s %v %v: got %+v ok=%v, want %q", c.goos, c.env, c.bins, ch, ok, c.want)
			continue
		}
		if ok && (len(ch.Argv) < 2 || filepath.Base(ch.Argv[0]) != c.want) {
			t.Errorf("%s: argv %q", c.want, ch.Argv)
		}
	}
	ch, _ := PickChooser("linux", envOf(gui), have("zenity"), "T", "/home/u")
	if s := strings.Join(ch.Argv, " "); !strings.Contains(s, "--multiple") || !strings.Contains(s, "--file-selection") {
		t.Errorf("zenity argv %q", s)
	}
}

func TestParseChosen(t *testing.T) {
	got := ParseChosen([]byte("/home/u/a b.txt\n\n/home/u/c.png\r\nrelative\n"))
	if len(got) != 2 || got[0] != "/home/u/a b.txt" || got[1] != "/home/u/c.png" {
		t.Errorf("got %q", got)
	}
}
