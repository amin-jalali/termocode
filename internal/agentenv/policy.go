package agentenv

import (
	"net"
	"strings"
)

// PackageHosts are the package registries of NetPackages (and of allow
// with package_managers).
var PackageHosts = []string{
	"registry.npmjs.org", "registry.yarnpkg.com", "registry.npmmirror.com",
	"pypi.org", "files.pythonhosted.org",
	"proxy.golang.org", "sum.golang.org", "index.golang.org",
	"crates.io", "static.crates.io", "index.crates.io",
	"rubygems.org", "index.rubygems.org",
	"repo.maven.apache.org", "repo1.maven.org", "plugins.gradle.org", "services.gradle.org",
	"api.nuget.org", "packagist.org", "repo.packagist.org",
}

// AgentHosts are the hosts an agent CLI needs for its own service; they
// pass every rule (else the agent cannot work at all).
func AgentHosts(agent string) []string {
	switch agent {
	case "claude-code":
		return []string{"api.anthropic.com", "statsig.anthropic.com", "console.anthropic.com", "claude.ai"}
	case "codex":
		return []string{"api.openai.com", "chatgpt.com", "auth.openai.com"}
	}
	return nil
}

// Policy decides which hosts pass.
type Policy struct {
	open  bool
	hosts []string // exact or *.domain
}

// Policy is the rule of n; always are hosts that pass every mode.
func (n Network) Policy(always ...string) Policy {
	p := Policy{hosts: append([]string(nil), always...)}
	switch n.Mode {
	case NetOff:
	case NetPackages:
		p.hosts = append(p.hosts, PackageHosts...)
	case NetAllow:
		p.hosts = append(p.hosts, n.Allow...)
		if n.PackageManagers {
			p.hosts = append(p.hosts, PackageHosts...)
		}
	default:
		p.open = true
	}
	return p
}

// Open reports a policy that passes every host (no proxy needed).
func (p Policy) Open() bool { return p.open }

// Allows reports whether host (maybe with :port) passes.
func (p Policy) Allows(host string) bool {
	if p.open {
		return true
	}
	h := normHost(host)
	if h == "" {
		return false
	}
	for _, a := range p.hosts {
		if rest, ok := strings.CutPrefix(a, "*."); ok {
			if strings.HasSuffix(h, "."+rest) {
				return true
			}
			continue
		}
		if h == a {
			return true
		}
	}
	return false
}

// normHost is host without port and final dot, lower case.
func normHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
}
