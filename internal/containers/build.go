package containers

import (
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/services"
)

// BuildOptions are the user's answers for one image build.
type BuildOptions struct {
	Dockerfile string   // the Dockerfile (or Containerfile) path
	Context    string   // build context folder; empty = the Dockerfile's folder
	Tag        string   // image reference; empty = no -t
	Args       []string // KEY=value build args
	Target     string   // stage name; empty = the last stage
	Platform   string   // e.g. linux/amd64; empty = the engine's default
}

// BuildCommand produces a visible terminal job for `<engine> build`. The
// argv is a plain list: no shell, so values with spaces stay one argument.
func (e *Engine) BuildCommand(o BuildOptions) services.Command {
	dir := o.Context
	if dir == "" {
		dir = filepath.Dir(o.Dockerfile)
	}
	argv := []string{e.Binary, "build", "-f", o.Dockerfile}
	if o.Tag != "" {
		argv = append(argv, "-t", o.Tag)
	}
	for _, a := range o.Args {
		argv = append(argv, "--build-arg", a)
	}
	if o.Target != "" {
		argv = append(argv, "--target", o.Target)
	}
	if o.Platform != "" {
		argv = append(argv, "--platform", o.Platform)
	}
	argv = append(argv, dir)
	label := o.Tag
	if label == "" {
		label = filepath.Base(o.Dockerfile)
	}
	return services.Command{Label: "build: " + label, Argv: argv, Dir: dir}
}

// IsDockerfileName reports a Dockerfile by its name: Dockerfile,
// Containerfile, Dockerfile.<x> or <x>.Dockerfile (any case of the suffix).
func IsDockerfileName(path string) bool {
	base := filepath.Base(path)
	switch {
	case base == "Dockerfile", base == "Containerfile":
		return true
	case strings.HasPrefix(base, "Dockerfile.") && !strings.HasSuffix(strings.ToLower(base), ".md"):
		return true
	case strings.HasSuffix(strings.ToLower(base), ".dockerfile"):
		return true
	}
	return false
}
