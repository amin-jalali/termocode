package session

import "strings"

// sshArgv is the ssh command line of one connection by `termocode ssh`:
// a terminal (-t), quick dead-link checks by ssh itself unless the user
// set their own, the user's arguments (destination included), then the
// far side's `termocode --link [folder]`. reconnected (may be empty) is
// the --reconnected value of a reconnect.
func sshArgv(args []string, remote, dir, reconnected string) []string {
	argv := []string{"-t"}
	if !hasSSHOption(args, "ServerAlive") {
		argv = append(argv, "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=3")
	}
	if !hasSSHOption(args, "ConnectTimeout") {
		argv = append(argv, "-o", "ConnectTimeout=10")
	}
	argv = append(argv, args...)
	cmd := []string{remote, "--link"}
	if reconnected != "" {
		cmd = append(cmd, "--reconnected="+reconnected)
	}
	if dir != "" {
		cmd = append(cmd, remotePath(dir))
	}
	return append(argv, "--", strings.Join(cmd, " "))
}

// hasSSHOption reports whether the user's ssh arguments set an option
// whose name starts with name (any case).
func hasSSHOption(args []string, name string) bool {
	name = strings.ToLower(name)
	for _, a := range args {
		if strings.Contains(strings.ToLower(a), name) {
			return true
		}
	}
	return false
}

// remotePath quotes a folder for the far side's shell; a leading ~/
// stays outside the quotes so the shell still expands it.
func remotePath(dir string) string {
	if dir == "~" {
		return dir
	}
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		return "~/" + shellQuote(rest)
	}
	return shellQuote(dir)
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
