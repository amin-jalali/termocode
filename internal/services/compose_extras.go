package services

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
)

// ResolvedConfig is `compose config`: the project as compose reads it,
// with variables, extends and profiles applied (YAML text).
func (c *Compose) ResolvedConfig(ctx context.Context) (string, error) {
	out, p := c.capture(ctx, 0, "config")
	if p.Kind != ProblemNone {
		return "", errors.New(p.Message())
	}
	return out, nil
}

// RunOneOff is `compose run --rm service args…` in a visible tab. The
// service must be a name, never a flag.
func (c *Compose) RunOneOff(service string, args []string) (Command, error) {
	service = strings.TrimSpace(service)
	if service == "" || strings.HasPrefix(service, "-") {
		return Command{}, errors.New("pick a service name")
	}
	argv := c.argv(append([]string{"run", "--rm", service}, args...)...)
	return Command{Label: "run: " + service, Argv: argv, Dir: c.Root}, nil
}

// WatchCommand is `compose watch`: rebuild or sync services when their files
// change (the develop.watch rules of the compose file).
func (c *Compose) WatchCommand() Command {
	return Command{Label: "services: watch", Argv: c.argv("watch"), Dir: c.Root}
}

// FindEnvFiles lists the env files in root: ".env", ".env.*" and
// "*.env" (plain files only, sorted).
func FindEnvFiles(root string) []string {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if !e.Type().IsRegular() {
			continue
		}
		if n == ".env" || strings.HasPrefix(n, ".env.") || strings.HasSuffix(n, ".env") {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
