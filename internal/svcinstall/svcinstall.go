// Package svcinstall installs the persistent session as a user service,
// so it (and the workflows in it) comes back after a reboot: a systemd
// user unit on Linux, a launchd LaunchAgent on macOS.
//
// Only per-user files under HOME are written; no sudo, no system-wide
// location. Every file carries a marker line, and uninstall removes only
// files that carry it.
package svcinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// Marker is written into every file we create.
const Marker = "termocode-service: managed by termocode, remove with `termocode service uninstall`"

// ErrCancelled means the user did not confirm the preview.
var ErrCancelled = errors.New("nothing was written")

// Spec says what to install and where.
type Spec struct {
	GOOS      string // runtime.GOOS
	Home      string // the user's HOME
	Exe       string // absolute path of the termocode binary
	Workspace string // absolute workspace folder
	UID       int    // macOS: the gui/<uid> domain; Linux: loginctl's user
	Termux    bool   // running inside Termux (see InTermux), even as GOOS linux
}

// InTermux reports whether termocode runs inside Termux on Android:
// TERMUX_VERSION is set by the Termux app, PREFIX is inside com.termux.
func InTermux(getenv func(string) string) bool {
	return getenv("TERMUX_VERSION") != "" || strings.Contains(getenv("PREFIX"), "com.termux")
}

// File is a rendered service file.
type File struct {
	Path    string
	Content string
	Name    string // unit name or launchd label
}

const unitTmpl = `# {{.Marker}}
[Unit]
Description=termocode session for {{.Workspace}}

[Service]
Type=simple
ExecStart="{{.Exe}}" __session-server "{{.Workspace}}"
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`

const plistTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- {{.Marker}} -->
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Name}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.Exe}}</string>
		<string>__session-server</string>
		<string>{{.Workspace}}</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
</dict>
</plist>
`

var (
	unitT  = template.Must(template.New("unit").Parse(unitTmpl))
	plistT = template.Must(template.New("plist").Parse(plistTmpl))
)

func unsupported(goos string) error {
	if goos == "android" {
		return errors.New("termocode service is not available in Termux: Android has no user services. Keep the session alive with termux-wake-lock instead")
	}
	return fmt.Errorf("termocode service supports Linux (systemd) and macOS (launchd), not %s", goos)
}

func safe(label, p string) error {
	if !filepath.IsAbs(p) {
		return fmt.Errorf("%s must be an absolute path: %q", label, p)
	}
	if strings.ContainsAny(p, "\"\\\n\r\x00") {
		return fmt.Errorf("%s has a quote, backslash or line break that cannot be written safely: %q", label, p)
	}
	return nil
}

func xmlEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func sysdEsc(s string) string {
	return strings.NewReplacer("%", "%%", "$", "$$").Replace(s)
}

// Render builds the service file for s without touching the disk.
func Render(s Spec) (File, error) {
	if s.Termux {
		return File{}, unsupported("android")
	}
	if s.GOOS != "linux" && s.GOOS != "darwin" {
		return File{}, unsupported(s.GOOS)
	}
	if s.Home == "" || !filepath.IsAbs(s.Home) {
		return File{}, errors.New("HOME is not set")
	}
	if err := safe("the termocode binary", s.Exe); err != nil {
		return File{}, err
	}
	if err := safe("the workspace", s.Workspace); err != nil {
		return File{}, err
	}
	sum := sha256.Sum256([]byte(s.Workspace))
	id := hex.EncodeToString(sum[:4])
	data := map[string]string{"Marker": Marker}
	var f File
	var b strings.Builder
	if s.GOOS == "linux" {
		f.Name = "termocode-" + id + ".service"
		f.Path = filepath.Join(s.Home, ".config", "systemd", "user", f.Name)
		data["Exe"], data["Workspace"] = sysdEsc(s.Exe), sysdEsc(s.Workspace)
		if err := unitT.Execute(&b, data); err != nil {
			return File{}, err
		}
	} else {
		f.Name = "dev.termocode." + id
		f.Path = filepath.Join(s.Home, "Library", "LaunchAgents", f.Name+".plist")
		data["Name"], data["Exe"], data["Workspace"] = f.Name, xmlEsc(s.Exe), xmlEsc(s.Workspace)
		if err := plistT.Execute(&b, data); err != nil {
			return File{}, err
		}
	}
	f.Content = b.String()
	return f, nil
}

func run(name string, args ...string) (string, error) {
	out, err := spawn.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func ctl(name string, args ...string) error {
	if out, err := run(name, args...); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, out)
	}
	return nil
}

func ours(path string) (exists, mine bool, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, strings.Contains(string(b), "termocode-service: managed"), nil
}

func writeAtomic(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".termocode-service-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Install shows the file, asks confirm (the default answer is no), then
// writes it and starts the service.
func Install(s Spec, w io.Writer, confirm func() bool) error {
	f, err := Render(s)
	if err != nil {
		return err
	}
	exists, mine, err := ours(f.Path)
	if err != nil {
		return err
	}
	if exists && !mine {
		return fmt.Errorf("%s exists and was not written by termocode; not touching it", f.Path)
	}
	fmt.Fprintf(w, "termocode will write this file (no sudo, user level only):\n%s\n\n%s\n", f.Path, f.Content)
	if !confirm() {
		return ErrCancelled
	}
	if err := writeAtomic(f.Path, f.Content); err != nil {
		return err
	}
	if s.GOOS == "linux" {
		if err := ctl("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		if err := ctl("systemctl", "--user", "enable", "--now", f.Name); err != nil {
			return err
		}
		if lingerOn(s.UID) {
			fmt.Fprintln(w, "Installed. It starts at boot, even when you are not logged in.")
			return nil
		}
		fmt.Fprintln(w, "Installed. "+lingerHint)
		return nil
	}
	if exists {
		_ = ctl("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", s.UID, f.Name))
	}
	if err := ctl("launchctl", "bootstrap", fmt.Sprintf("gui/%d", s.UID), f.Path); err != nil {
		return err
	}
	fmt.Fprintln(w, "Installed. It starts at every login.")
	return nil
}

// Uninstall stops the service and removes the file, only if we wrote it.
func Uninstall(s Spec, w io.Writer) error {
	f, err := Render(s)
	if err != nil {
		return err
	}
	exists, mine, err := ours(f.Path)
	if err != nil {
		return err
	}
	if !exists {
		fmt.Fprintln(w, "Not installed. Nothing to do.")
		return nil
	}
	if !mine {
		return fmt.Errorf("%s was not written by termocode; not removing it", f.Path)
	}
	if s.GOOS == "linux" {
		_ = ctl("systemctl", "--user", "disable", "--now", f.Name)
	} else {
		_ = ctl("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", s.UID, f.Name))
	}
	if err := os.Remove(f.Path); err != nil {
		return err
	}
	if s.GOOS == "linux" {
		_ = ctl("systemctl", "--user", "daemon-reload")
	}
	fmt.Fprintln(w, "Removed "+f.Path)
	return nil
}

// Status says whether the file exists and the service runs.
func Status(s Spec, w io.Writer) error {
	f, err := Render(s)
	if err != nil {
		return err
	}
	exists, mine, err := ours(f.Path)
	if err != nil {
		return err
	}
	switch {
	case !exists:
		fmt.Fprintln(w, "not installed ("+f.Path+")")
		return nil
	case !mine:
		fmt.Fprintln(w, "a file not written by termocode is at "+f.Path)
		return nil
	}
	fmt.Fprintln(w, "installed: "+f.Path)
	var out string
	if s.GOOS == "linux" {
		out, _ = run("systemctl", "--user", "is-active", f.Name)
	} else if _, err := run("launchctl", "print", fmt.Sprintf("gui/%d/%s", s.UID, f.Name)); err == nil {
		out = "active"
	} else {
		out = "inactive"
	}
	fmt.Fprintln(w, "state: "+out)
	if s.GOOS == "linux" && !lingerOn(s.UID) {
		fmt.Fprintln(w, lingerHint)
	}
	return nil
}

const lingerHint = "It starts when you log in. To start it at boot even when you are not logged in, run once:\n  loginctl enable-linger $USER"

// lingerOn asks loginctl whether the user's services run without a
// login (linger). No loginctl or no answer counts as off.
func lingerOn(uid int) bool {
	out, err := run("loginctl", "show-user", strconv.Itoa(uid), "--property=Linger", "--value")
	return err == nil && strings.TrimSpace(out) == "yes"
}
