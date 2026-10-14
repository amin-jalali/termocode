package transfer

import (
	"strings"
)

// sshTarget is "user@host" as the user's machine reaches this one: the
// server address of SSH_CONNECTION when set, else hostname.
func sshTarget(getenv func(string) string, hostname string) string {
	user := getenv("USER")
	if user == "" {
		user = getenv("LOGNAME")
	}
	host := hostname
	if f := strings.Fields(getenv("SSH_CONNECTION")); len(f) >= 3 {
		host = f[2]
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if user == "" {
		return host
	}
	return user + "@" + host
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ScpDownload is the command the user runs on their machine to copy abs
// from here into their current folder.
func ScpDownload(getenv func(string) string, hostname, abs string, dir bool) string {
	flag := ""
	if dir {
		flag = "-r "
	}
	return "scp " + flag + shellQuote(sshTarget(getenv, hostname)+":"+abs) + " ."
}

// ScpUpload is the command that copies FILE from the user's machine into
// the folder dir here.
func ScpUpload(getenv func(string) string, hostname, dir string) string {
	return "scp -r FILE " + shellQuote(sshTarget(getenv, hostname)+":"+strings.TrimSuffix(dir, "/")+"/")
}
