package containers

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ContextSpec is a new docker context (podman: a system connection).
// Host is the endpoint: ssh://user@host, tcp://host:port or unix:///path.
// CA, Cert and Key are TLS file paths for a tcp endpoint; for podman, Key
// is the ssh identity file. No secret material is stored here: only paths.
type ContextSpec struct {
	Name, Description, Host string
	CA, Cert, Key           string
	SkipTLSVerify           bool
}

// contextName checks a context name: it becomes one argv item, so it may
// not look like a flag; "default" is the engine's own and cannot change.
func contextName(name string) error {
	switch {
	case name == "":
		return errors.New("the context needs a name")
	case name == "default":
		return errors.New(`"default" is the built-in context and cannot be changed`)
	case strings.HasPrefix(name, "-"):
		return errors.New("a context name cannot start with -")
	}
	for _, c := range name {
		if !(c == '-' || c == '_' || c == '.' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return errors.New("a context name may use only letters, digits, '.', '-' and '_'")
		}
	}
	return nil
}

// CreateContextArgs is the argv (without the binary) that creates spec.
func (e *Engine) CreateContextArgs(spec ContextSpec) ([]string, error) {
	if err := contextName(spec.Name); err != nil {
		return nil, err
	}
	scheme, _, ok := strings.Cut(spec.Host, "://")
	if !ok || spec.Host == "" {
		return nil, errors.New("the host must start with ssh://, tcp:// or unix://")
	}
	switch scheme {
	case "ssh", "tcp", "unix":
	default:
		return nil, errors.New("the host must start with ssh://, tcp:// or unix://")
	}
	tls := spec.CA != "" || spec.Cert != "" || spec.SkipTLSVerify
	if e.Kind == "podman" {
		if tls {
			return nil, errors.New("podman connections do not take TLS files")
		}
		args := []string{"system", "connection", "add"}
		if spec.Key != "" {
			if strings.HasPrefix(spec.Key, "-") {
				return nil, errors.New("the identity file path cannot start with -")
			}
			args = append(args, "--identity", spec.Key)
		}
		return append(args, spec.Name, spec.Host), nil
	}
	if (tls || spec.Key != "") && scheme != "tcp" {
		return nil, errors.New("TLS files work only with a tcp:// host")
	}
	// --docker takes key=value pairs split at commas: a comma or '=' inside
	// a value would add a pair the user did not write.
	parts := []string{"host=" + spec.Host}
	for _, kv := range [][2]string{{"ca", spec.CA}, {"cert", spec.Cert}, {"key", spec.Key}} {
		if kv[1] != "" {
			parts = append(parts, kv[0]+"="+kv[1])
		}
	}
	for _, v := range []string{spec.Host, spec.CA, spec.Cert, spec.Key} {
		if strings.ContainsAny(v, ",=\n") {
			return nil, errors.New("hosts and file paths cannot contain a comma or '='")
		}
	}
	if spec.SkipTLSVerify {
		parts = append(parts, "skip-tls-verify=true")
	}
	args := []string{"context", "create", spec.Name}
	if spec.Description != "" {
		args = append(args, "--description", spec.Description)
	}
	return append(args, "--docker", strings.Join(parts, ",")), nil
}

// RemoveContextArgs is the argv that removes the context name (no force:
// the engine refuses a context in use).
func (e *Engine) RemoveContextArgs(name string) ([]string, error) {
	if err := contextName(name); err != nil {
		return nil, err
	}
	if e.Kind == "podman" {
		return []string{"system", "connection", "remove", name}, nil
	}
	return []string{"context", "rm", name}, nil
}

// InspectContextArgs is the argv that prints the context name as JSON.
func (e *Engine) InspectContextArgs(name string) ([]string, error) {
	if name == "" || strings.HasPrefix(name, "-") {
		return nil, errors.New("not a context name")
	}
	if e.Kind == "podman" {
		return nil, errors.New("podman has no connection inspect; the list shows each connection")
	}
	return []string{"context", "inspect", name}, nil
}

// plainEngine is e without the session's context: context management is
// machine-wide, like UseContext.
func (e *Engine) plainEngine() *Engine {
	plain := *e
	plain.Context = ""
	return &plain
}

// CreateContext creates a context from spec.
func (e *Engine) CreateContext(ctx context.Context, spec ContextSpec) error {
	args, err := e.CreateContextArgs(spec)
	if err != nil {
		return err
	}
	return e.plainEngine().action(ctx, 60*time.Second, args...)
}

// RemoveContext removes the context name.
func (e *Engine) RemoveContext(ctx context.Context, name string) error {
	args, err := e.RemoveContextArgs(name)
	if err != nil {
		return err
	}
	return e.plainEngine().action(ctx, 60*time.Second, args...)
}

// InspectContext returns the JSON the engine prints for the context name.
func (e *Engine) InspectContext(ctx context.Context, name string) (string, error) {
	args, err := e.InspectContextArgs(name)
	if err != nil {
		return "", err
	}
	return e.plainEngine().list(ctx, args...)
}
