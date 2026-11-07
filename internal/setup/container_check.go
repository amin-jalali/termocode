package setup

import (
	"net/url"
	"strings"
)

// Container workspaces over ssh (plan RC, phase 8): every engine call on
// an ssh:// context opens an ssh session. termocode keeps one shell open
// per container workspace (the shellmux), but the first call and every
// copy still pay a new session unless ssh reuses one (ControlMaster).
// The check is read-only: `docker context ls` and `ssh -G` print config,
// they change nothing and connect to nothing.

// sshContext is one docker context reached over ssh.
type sshContext struct {
	Name     string
	Endpoint string // ssh://user@host:port
}

// dockerSSHContexts lists the docker contexts with an ssh:// endpoint.
func dockerSSHContexts() []sshContext {
	out, err := runOutput("docker", "context", "ls", "--format", "{{.Name}}\t{{.DockerEndpoint}}")
	if err != nil {
		return nil
	}
	return parseSSHContexts(out)
}

// parseSSHContexts reads `docker context ls` lines "name\tendpoint".
func parseSSHContexts(out string) []sshContext {
	var cs []sshContext
	for _, line := range strings.Split(out, "\n") {
		name, ep, ok := strings.Cut(strings.TrimSpace(line), "\t")
		name = strings.TrimSuffix(strings.TrimSpace(name), " *") // the current one
		ep = strings.TrimSpace(ep)
		if !ok || name == "" || !strings.HasPrefix(strings.ToLower(ep), "ssh://") {
			continue
		}
		cs = append(cs, sshContext{Name: name, Endpoint: ep})
	}
	return cs
}

// sshTarget is the `ssh -G` arguments of an ssh:// endpoint: [-p port]
// [user@]host. ok is false for an endpoint that is not a plain host
// (nothing then reaches ssh as an option).
func sshTarget(endpoint string) ([]string, bool) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "ssh" || u.Hostname() == "" || strings.HasPrefix(u.Hostname(), "-") {
		return nil, false
	}
	target := u.Hostname()
	if u.User != nil && u.User.Username() != "" {
		if strings.HasPrefix(u.User.Username(), "-") {
			return nil, false
		}
		target = u.User.Username() + "@" + target
	}
	var args []string
	if p := u.Port(); p != "" {
		args = append(args, "-p", p)
	}
	return append(args, "--", target), true
}

// controlMasterOn reads `ssh -G` output: ControlMaster reuses sessions
// when it is on and has a ControlPath.
func controlMasterOn(sshG string) bool {
	master, cpath := "", ""
	for _, line := range strings.Split(sshG, "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch strings.ToLower(k) {
		case "controlmaster":
			master = strings.ToLower(strings.TrimSpace(v))
		case "controlpath":
			cpath = strings.TrimSpace(v)
		}
	}
	on := master == "auto" || master == "autoask" || master == "yes" || master == "ask" || master == "true"
	return on && cpath != "" && cpath != "none"
}

// controlMasterHint is the ~/.ssh/config block the check suggests.
func controlMasterHint(host string) string {
	return "slow container workspace: add to ~/.ssh/config: Host " + host +
		" / ControlMaster auto / ControlPath ~/.ssh/cm-%r@%h:%p / ControlPersist 10m"
}

// ContainerSSHChecks is one check per docker context over ssh: whether
// ssh reuses its connection. None when docker is missing or no context
// uses ssh.
func ContainerSSHChecks() []Check {
	if _, err := lookPath("docker"); err != nil {
		return nil
	}
	if _, err := lookPath("ssh"); err != nil {
		return nil
	}
	var out []Check
	for _, c := range dockerSSHContexts() {
		chk := Check{Group: "containers", Name: "ssh context " + c.Name}
		args, ok := sshTarget(c.Endpoint)
		if !ok {
			chk.Status, chk.Detail = Warn, "cannot read the endpoint "+c.Endpoint
			out = append(out, chk)
			continue
		}
		g, err := runOutput("ssh", append([]string{"-G"}, args...)...)
		host := args[len(args)-1]
		if at := strings.LastIndexByte(host, '@'); at >= 0 {
			host = host[at+1:]
		}
		switch {
		case err != nil:
			chk.Status, chk.Detail = Warn, "cannot read the ssh config for "+host
		case controlMasterOn(g):
			chk.Status, chk.Detail = OK, "ControlMaster on · "+c.Endpoint
		default:
			chk.Status, chk.Detail = Warn, controlMasterHint(host)
		}
		out = append(out, chk)
	}
	return out
}
