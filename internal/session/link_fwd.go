package session

import (
	"strconv"
	"strings"
)

// Port preview over `termocode ssh --web` (WEB-54). After the user said
// yes on the far UI's card, the far UI asks the local termocode ssh to
// forward one port of the far host's loopback, in-band, as one private
// APC string (removed from the output by the same filter as the web
// hand-off, so no terminal shows it):
//
//	ESC _ termocode:fwd;1;<port>;<id> ESC \     open: forward far 127.0.0.1:<port>
//	ESC _ termocode:fwd;0;<id> ESC \            close the forward <id>
//
// There is no host field: the far end of a forward is always 127.0.0.1
// on the far host, and the near end always 127.0.0.1 on this computer,
// on a port of its own (a different origin than the web panel's).

const fwdAPCPrefix = "\x1b_termocode:fwd;"

// maxFwdID bounds a forward's id.
const maxFwdID = 16

// PortForward is one forward request of the far UI.
type PortForward struct {
	Open bool   // open (true) or close (false)
	Port int    // the far loopback port (open only)
	ID   string // the far UI's name for the forward: [a-z0-9], 1..16
}

// PortForwardAPC encodes f for the far UI's output ("" for a bad f).
func PortForwardAPC(f PortForward) string {
	if !goodFwdID(f.ID) {
		return ""
	}
	if !f.Open {
		return fwdAPCPrefix + "0;" + f.ID + apcEnd
	}
	if f.Port < 1 || f.Port > 65535 {
		return ""
	}
	return fwdAPCPrefix + "1;" + strconv.Itoa(f.Port) + ";" + f.ID + apcEnd
}

// parsePortForward reads the payload between the prefix and the end.
func parsePortForward(p []byte) (PortForward, bool) {
	parts := strings.Split(string(p), ";")
	switch {
	case len(parts) == 2 && parts[0] == "0" && goodFwdID(parts[1]):
		return PortForward{ID: parts[1]}, true
	case len(parts) == 3 && parts[0] == "1" && goodFwdID(parts[2]):
		port, err := strconv.Atoi(parts[1])
		if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != parts[1] {
			return PortForward{}, false
		}
		return PortForward{Open: true, Port: port, ID: parts[2]}, true
	}
	return PortForward{}, false
}

func goodFwdID(s string) bool {
	if s == "" || len(s) > maxFwdID {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// SSHPortForwardArgv is the ssh command line of one port preview: like
// SSHForwardArgv (no command, no terminal, BatchMode, no shared master),
// with one forward from 127.0.0.1:local here to 127.0.0.1:remote on the
// far host. Both ends are loopback; there is no way to name another host.
func SSHPortForwardArgv(args []string, local, remote int) []string {
	argv := []string{"-N", "-T", "-o", "ExitOnForwardFailure=yes", "-o", "ControlPath=none", "-o", "BatchMode=yes"}
	if !hasSSHOption(args, "ServerAlive") {
		argv = append(argv, "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=3")
	}
	if !hasSSHOption(args, "ConnectTimeout") {
		argv = append(argv, "-o", "ConnectTimeout=10")
	}
	argv = append(argv, "-L", "127.0.0.1:"+strconv.Itoa(local)+":127.0.0.1:"+strconv.Itoa(remote))
	return append(argv, args...)
}
