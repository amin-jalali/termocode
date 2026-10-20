package dbclient

import (
	"bufio"
	"bytes"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// sshHostConfig is the subset of ~/.ssh/config the tunnel reads for one
// host: HostName, Port, User, IdentityFile and whether a jump host
// (ProxyJump / ProxyCommand) is set. The first value of a keyword wins,
// as in OpenSSH.
type sshHostConfig struct {
	HostName      string
	Port          int
	User          string
	IdentityFiles []string
	ProxyJump     string
}

// parseSSHConfig reads the blocks of data that match alias. Match blocks
// and Include lines are skipped (not supported).
func parseSSHConfig(data []byte, alias string) sshHostConfig {
	var out sshHostConfig
	seen := map[string]bool{}
	active := true // lines before the first Host apply to every host
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, rest := splitSSHConfigLine(line)
		args := sshConfigArgs(rest)
		switch key {
		case "host":
			active = sshHostMatches(args, alias)
			continue
		case "match":
			active = false
			continue
		}
		if !active || len(args) == 0 {
			continue
		}
		switch key {
		case "identityfile":
			out.IdentityFiles = append(out.IdentityFiles, args[0])
			continue
		case "hostname", "port", "user", "proxyjump", "proxycommand":
		default:
			continue
		}
		if seen[key] || (key == "proxycommand" && seen["proxyjump"]) || (key == "proxyjump" && seen["proxycommand"]) {
			continue
		}
		seen[key] = true
		switch key {
		case "hostname":
			out.HostName = strings.ReplaceAll(args[0], "%h", alias)
		case "port":
			if p, err := strconv.Atoi(args[0]); err == nil && p > 0 && p < 65536 {
				out.Port = p
			}
		case "user":
			out.User = args[0]
		case "proxyjump", "proxycommand":
			if !strings.EqualFold(args[0], "none") {
				out.ProxyJump = strings.Join(args, " ")
			}
		}
	}
	return out
}

func splitSSHConfigLine(line string) (string, string) {
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return strings.ToLower(line), ""
	}
	key := strings.ToLower(line[:i])
	rest := strings.TrimLeft(line[i:], " \t")
	rest = strings.TrimPrefix(rest, "=")
	return key, strings.TrimSpace(rest)
}

// sshConfigArgs splits on blanks; double quotes group words.
func sshConfigArgs(s string) []string {
	var out []string
	var b strings.Builder
	quoted, have := false, false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			have = true
		case (r == ' ' || r == '\t') && !quoted:
			if have {
				out = append(out, b.String())
				b.Reset()
				have = false
			}
		default:
			b.WriteRune(r)
			have = true
		}
	}
	if have {
		out = append(out, b.String())
	}
	return out
}

// sshHostMatches applies OpenSSH Host patterns (*, ?, !negation; split on
// commas too) to alias.
func sshHostMatches(patterns []string, alias string) bool {
	alias = strings.ToLower(alias)
	matched := false
	for _, field := range patterns {
		for _, p := range strings.Split(field, ",") {
			if p == "" {
				continue
			}
			neg := strings.HasPrefix(p, "!")
			p = strings.ToLower(strings.TrimPrefix(p, "!"))
			ok, err := path.Match(p, alias)
			if err != nil || !ok {
				continue
			}
			if neg {
				return false
			}
			matched = true
		}
	}
	return matched
}

// expandSSHPath expands ~, %d (home), %h (host), %r (remote user) and %%
// in an IdentityFile / known_hosts path.
func expandSSHPath(p, home, host, user string) string {
	if p == "~" {
		p = home
	} else if strings.HasPrefix(p, "~/") {
		p = filepath.Join(home, p[2:])
	}
	if strings.Contains(p, "%") {
		var b strings.Builder
		for i := 0; i < len(p); i++ {
			if p[i] != '%' || i+1 >= len(p) {
				b.WriteByte(p[i])
				continue
			}
			i++
			switch p[i] {
			case 'd':
				b.WriteString(home)
			case 'h':
				b.WriteString(host)
			case 'r':
				b.WriteString(user)
			case '%':
				b.WriteByte('%')
			default:
				b.WriteByte('%')
				b.WriteByte(p[i])
			}
		}
		p = b.String()
	}
	return p
}
