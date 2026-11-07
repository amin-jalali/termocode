package containers

import (
	"errors"
	"strings"
	"unicode"

	"github.com/amin-jalali/termocode/internal/services"
)

// ExecOptions are the user's choices for "exec into a container".
// Command, when set, runs as argv (no shell). Otherwise Shell runs; an
// empty Shell keeps the bash-or-sh default of Engine.Shell.
type ExecOptions struct {
	Shell   string
	Command []string
	User    string
	Workdir string
}

// execWord is a single argv word that cannot be read as an option and
// has no spaces or control characters.
func execWord(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// ExecCommand builds a visible terminal job that runs exec in container
// id with the given options.
func (e *Engine) ExecCommand(id string, o ExecOptions) (services.Command, error) {
	if !execWord(id) {
		return services.Command{}, errors.New("exec: bad container id")
	}
	if o.User != "" && !execWord(o.User) {
		return services.Command{}, errors.New("exec: bad user")
	}
	if o.Workdir != "" && (!execWord(o.Workdir) || !strings.HasPrefix(o.Workdir, "/")) {
		return services.Command{}, errors.New("exec: workdir must be an absolute path")
	}
	var run []string
	switch {
	case len(o.Command) > 0:
		if o.Command[0] == "" || strings.HasPrefix(o.Command[0], "-") {
			return services.Command{}, errors.New("exec: bad command")
		}
		for _, a := range o.Command {
			if strings.ContainsFunc(a, unicode.IsControl) {
				return services.Command{}, errors.New("exec: bad command")
			}
		}
		run = o.Command
	case o.Shell != "":
		if !execWord(o.Shell) {
			return services.Command{}, errors.New("exec: bad shell")
		}
		run = []string{o.Shell}
	}
	if run == nil && o.User == "" && o.Workdir == "" {
		return e.Shell(id), nil
	}
	argv := []string{e.Binary, "exec", "-it"}
	if o.User != "" {
		argv = append(argv, "--user", o.User)
	}
	if o.Workdir != "" {
		argv = append(argv, "--workdir", o.Workdir)
	}
	argv = append(argv, id)
	label := "shell: " + id
	if run == nil {
		argv = append(argv, "sh", "-c", "command -v bash >/dev/null && exec bash || exec sh")
	} else {
		argv = append(argv, run...)
		label = "exec: " + id + " " + strings.Join(run, " ")
	}
	return services.Command{Label: label, Argv: argv}, nil
}
