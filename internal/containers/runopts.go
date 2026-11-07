package containers

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/amin-jalali/termocode/internal/services"
)

// RunOptions are the full choices of "Run from image" and the copied
// settings of "Duplicate / Recreate container".
type RunOptions struct {
	Image      string
	Name       string
	Detach     bool
	Remove     bool
	Ports      []string
	Volumes    []string
	Env        map[string]string
	Network    string
	Restart    string
	User       string
	Workdir    string
	Hostname   string
	Platform   string
	Labels     map[string]string
	Entrypoint string
	Command    []string
}

// runWord is one argv value that cannot be read as an option and has
// no spaces or control characters.
func runWord(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func validRestart(s string) bool {
	switch s {
	case "", "no", "always", "unless-stopped", "on-failure":
		return true
	}
	n, ok := strings.CutPrefix(s, "on-failure:")
	if !ok {
		return false
	}
	_, err := strconv.ParseUint(n, 10, 16)
	return err == nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// RunOptionsCommand builds the visible terminal job for o. Env values
// go into the job's process env and argv names them only (-e NAME), so
// values never show in argv.
func (e *Engine) RunOptionsCommand(o RunOptions) (services.Command, error) {
	bad := func(what string) (services.Command, error) {
		return services.Command{}, fmt.Errorf("run: bad %s", what)
	}
	if !runWord(o.Image) {
		return bad("image")
	}
	for what, v := range map[string]string{"name": o.Name, "network": o.Network, "user": o.User, "hostname": o.Hostname, "platform": o.Platform, "entrypoint": o.Entrypoint} {
		if v != "" && !runWord(v) {
			return bad(what)
		}
	}
	if o.Workdir != "" && (!runWord(o.Workdir) || !strings.HasPrefix(o.Workdir, "/")) {
		return bad("workdir")
	}
	if !validRestart(o.Restart) {
		return bad("restart policy")
	}
	argv := []string{e.Binary, "run"}
	if o.Remove {
		argv = append(argv, "--rm")
	}
	if o.Detach {
		argv = append(argv, "-d")
	} else {
		argv = append(argv, "-it")
	}
	if o.Name != "" {
		argv = append(argv, "--name", o.Name)
	}
	for _, p := range o.Ports {
		if !runWord(p) {
			return bad("port")
		}
		argv = append(argv, "-p", p)
	}
	for _, v := range o.Volumes {
		if !runWord(v) {
			return bad("volume")
		}
		argv = append(argv, "-v", v)
	}
	var env map[string]string
	for _, k := range sortedKeys(o.Env) {
		if !runWord(k) || strings.Contains(k, "=") {
			return bad("env name")
		}
		if env == nil {
			env = map[string]string{}
		}
		env[k] = o.Env[k]
		argv = append(argv, "-e", k)
	}
	for _, kv := range [][2]string{{"--network", o.Network}, {"--restart", o.Restart}, {"--user", o.User}, {"--workdir", o.Workdir}, {"--hostname", o.Hostname}, {"--platform", o.Platform}} {
		if kv[1] != "" {
			argv = append(argv, kv[0], kv[1])
		}
	}
	for _, k := range sortedKeys(o.Labels) {
		v := o.Labels[k]
		if !runWord(k) || strings.Contains(k, "=") || strings.ContainsFunc(v, unicode.IsControl) {
			return bad("label")
		}
		argv = append(argv, "--label", k+"="+v)
	}
	if o.Entrypoint != "" {
		argv = append(argv, "--entrypoint", o.Entrypoint)
	}
	argv = append(argv, o.Image)
	for _, a := range o.Command {
		if strings.ContainsFunc(a, unicode.IsControl) {
			return bad("command")
		}
	}
	argv = append(argv, o.Command...)
	return services.Command{Label: "run: " + o.Image, Argv: argv, Env: env}, nil
}

type inspectRun struct {
	Name   string
	Config struct {
		Image      string
		Env        []string
		Cmd        []string
		Entrypoint []string
		WorkingDir string
		User       string
		Labels     map[string]string
	}
	HostConfig struct {
		Binds         []string
		NetworkMode   string
		AutoRemove    bool
		RestartPolicy struct{ Name string }
		PortBindings  map[string][]struct{ HostIp, HostPort string }
	}
}

// RunOptionsFromInspect reads `docker inspect <container>` output and
// returns options that start an equal container (Duplicate, Recreate).
// The generated hostname is not copied.
func RunOptionsFromInspect(b []byte) (RunOptions, error) {
	var list []inspectRun
	if err := json.Unmarshal(b, &list); err != nil || len(list) == 0 {
		return RunOptions{}, errors.New("duplicate: not container inspect output")
	}
	c := list[0]
	if c.Config.Image == "" {
		return RunOptions{}, errors.New("duplicate: container has no image")
	}
	o := RunOptions{
		Image:   c.Config.Image,
		Name:    strings.TrimPrefix(c.Name, "/"),
		Detach:  true,
		Remove:  c.HostConfig.AutoRemove,
		Volumes: c.HostConfig.Binds,
		User:    c.Config.User,
		Workdir: c.Config.WorkingDir,
		Command: c.Config.Cmd,
		Labels:  c.Config.Labels,
	}
	if r := c.HostConfig.RestartPolicy.Name; r != "" && r != "no" {
		o.Restart = r
	}
	switch n := c.HostConfig.NetworkMode; n {
	case "", "default", "bridge":
	default:
		o.Network = n
	}
	if len(c.Config.Entrypoint) > 0 {
		o.Entrypoint = c.Config.Entrypoint[0]
		o.Command = append(append([]string{}, c.Config.Entrypoint[1:]...), c.Config.Cmd...)
	}
	for _, kv := range c.Config.Env {
		k, v, _ := strings.Cut(kv, "=")
		if k == "" {
			continue
		}
		if o.Env == nil {
			o.Env = map[string]string{}
		}
		o.Env[k] = v
	}
	ports := make([]string, 0, len(c.HostConfig.PortBindings))
	for cport, binds := range c.HostConfig.PortBindings {
		for _, b := range binds {
			p := b.HostPort + ":" + cport
			if b.HostIp != "" {
				p = b.HostIp + ":" + p
			}
			ports = append(ports, p)
		}
	}
	sort.Strings(ports)
	if len(ports) > 0 {
		o.Ports = ports
	}
	return o, nil
}

// DuplicateName returns name-copy, or name-copy-N, not in taken.
func DuplicateName(name string, taken []string) string {
	used := make(map[string]bool, len(taken))
	for _, t := range taken {
		used[t] = true
	}
	base := name + "-copy"
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		if c := base + "-" + strconv.Itoa(i); !used[c] {
			return c
		}
	}
}
