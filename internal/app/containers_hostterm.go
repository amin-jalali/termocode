package app

import (
	"net/url"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Open Terminal on a host row (CONTAINERS' hosts tree, Databases'
// SERVERS, the web's host rows): an ssh:// host opens a terminal tab
// running ssh to it, as argv (no shell reads it): user, host and port
// come from the context URI, the key from the ssh-agent and
// ~/.ssh/config as for any ssh. The local host opens a plain shell in
// the project folder. Container rows keep their own exec terminal.

// hostTermTabPrefix keys a host's terminal tab (one per host).
const hostTermTabPrefix = "host-term:"

// spawnHostTerminal opens the tab (tests replace it: ssh never runs).
var spawnHostTerminal = func(m *Model, j terminalJob) error {
	_, err := m.spawnTerminalJob(j)
	return err
}

var (
	sshUserRe = lazyre.New(`^[A-Za-z0-9._][A-Za-z0-9._-]{0,63}$`)
	sshHostRe = lazyre.New(`^[A-Za-z0-9_][A-Za-z0-9._:-]{0,252}$`)
)

// hostTerminalArgv is the ssh argv of an ssh:// endpoint ("--" before
// the destination, and a user or host that could read as an option is
// refused). ok=false: not ssh://, or not a safe host.
func hostTerminalArgv(endpoint string) ([]string, bool) {
	if !containerhost.SSHEndpoint(endpoint) {
		return nil, false
	}
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Hostname() == "" || !sshHostRe.MatchString(u.Hostname()) {
		return nil, false
	}
	dest := u.Hostname()
	if u.User != nil && u.User.Username() != "" {
		if !sshUserRe.MatchString(u.User.Username()) {
			return nil, false
		}
		dest = u.User.Username() + "@" + dest
	}
	argv := []string{"ssh"}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n > 65535 {
			return nil, false
		}
		if n != 22 {
			argv = append(argv, "-p", strconv.Itoa(n))
		}
	}
	return append(argv, "--", dest), true
}

// localShell is the user's shell ($SHELL, else /bin/sh).
func localShell() string {
	if s := strings.TrimSpace(os.Getenv("SHELL")); s != "" && !strings.ContainsAny(s, " \t\n") {
		return s
	}
	return "/bin/sh"
}

// hostIsLocal: the endpoint is this machine (a socket, or none).
func hostIsLocal(endpoint string) bool {
	e := strings.ToLower(strings.TrimSpace(endpoint))
	return e == "" || strings.HasPrefix(e, "unix://") || strings.HasPrefix(e, "npipe://") || e == "this machine (no connection)"
}

// openHostTerminal is Open Terminal on the host name (endpoint: where it
// points; "" and a socket: this machine).
func (m *Model) openHostTerminal(name, endpoint string) tea.Cmd {
	key := hostTermTabPrefix + name
	if m.servicesFocusTab(key) {
		return nil
	}
	j := terminalJob{Label: "terminal: " + contextDisplayName(name), Service: key}
	switch argv, ok := hostTerminalArgv(endpoint); {
	case ok:
		j.Argv, j.Label = argv, "ssh: "+contextDisplayName(name)
	case hostIsLocal(endpoint):
		j.Argv, j.Dir = []string{localShell()}, m.hostTerminalDir()
	case containerhost.SSHEndpoint(endpoint):
		return m.pushToastDetail(toast.Errr, "Cannot open a terminal", "The host of "+contextDisplayName(name)+" is not a plain user@host.")
	default:
		return m.pushToastDetail(toast.Info, "No terminal for this host", "Open Terminal needs an ssh:// host or this machine.")
	}
	if err := spawnHostTerminal(m, j); err != nil {
		return m.pushToastDetail(toast.Errr, "Cannot open a terminal", sanitizeOutputLine(err.Error()))
	}
	return nil
}

// hostTerminalDir is the project folder.
func (m *Model) hostTerminalDir() string {
	if st := m.services; st != nil && st.root != "" {
		return st.root
	}
	return m.taskVars().WorkspaceFolder
}

// openContextTerminal is Open Terminal on a host row of CONTAINERS.
func (m *Model) openContextTerminal(name string) tea.Cmd {
	st := m.services
	if st == nil {
		return nil
	}
	return m.openHostTerminal(name, st.serverContextOf(name).DockerEndpoint)
}
