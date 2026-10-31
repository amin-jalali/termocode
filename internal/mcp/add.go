package mcp

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// Guided add of a local (stdio) server (DI-7, DI-12) and the install
// text of `termocode mcp add <url|json>` (DI-5). The Connect wizard and
// the CLI both build entries here, so they write the same mcp.json.

// Kinds of the quick form: a command, or a package that a runner starts.
const (
	KindCommand = "command" // Command is the program
	KindNPM     = "npm"     // npx -y <package>
	KindPip     = "pip"     // uvx <package>
	KindImage   = "image"   // docker run -i --rm <image>
)

// LocalSpec is what the form or the CLI asks for one local server.
type LocalSpec struct {
	Name    string
	Kind    string            // "" = KindCommand
	Command string            // the program, or the package / image
	Args    []string          // more arguments (after the package)
	Env     map[string]string // plain values (never a token)
	Secrets []string          // env names whose value is a stored secret
	Share   string            // ShareUser: one process for every project
}

var (
	envNameRe = lazyre.New(`^[A-Za-z_][A-Za-z0-9_]*$`)
	packageRe = lazyre.New(`^[A-Za-z0-9@._/:+=~-]+$`)
)

// LocalSecretName is the store name of an env secret of a server
// ("pg", "PGPASSWORD" → "pg_pgpassword").
func LocalSecretName(server, env string) string { return secretName(server, env) }

// SecretNames lists the store names of the spec's secrets, sorted.
func (s LocalSpec) SecretNames() []string {
	out := make([]string, 0, len(s.Secrets))
	for _, n := range s.Secrets {
		out = append(out, LocalSecretName(s.Name, n))
	}
	sort.Strings(out)
	return out
}

// Entry is the mcp.json entry of the spec. Secrets become
// ${secret:NAME}; their values are stored by the caller.
func (s LocalSpec) Entry() (map[string]any, error) {
	if !nameRe.MatchString(s.Name) {
		return nil, errors.New("name may only use letters, digits, - and _ (40 at most)")
	}
	cmd := strings.TrimSpace(s.Command)
	if cmd == "" {
		return nil, errors.New("needs a command")
	}
	kind := s.Kind
	if kind == "" {
		kind = KindCommand
	}
	if kind != KindCommand && (!packageRe.MatchString(cmd) || strings.HasPrefix(cmd, "-")) {
		return nil, fmt.Errorf("%q is not a package or image name", cmd)
	}
	env := map[string]string{}
	var envNames []string
	for k, v := range s.Env {
		if !envNameRe.MatchString(k) {
			return nil, fmt.Errorf("%q is not an environment variable name", k)
		}
		if secretLiteralRe.MatchString(refRe.ReplaceAllString(v, "")) {
			return nil, fmt.Errorf("the value of %s looks like a token: add it as a secret", k)
		}
		env[k] = v
		envNames = append(envNames, k)
	}
	for _, k := range s.Secrets {
		if !envNameRe.MatchString(k) {
			return nil, fmt.Errorf("%q is not an environment variable name", k)
		}
		if _, dup := env[k]; dup {
			return nil, fmt.Errorf("%s is set twice", k)
		}
		env[k] = "${secret:" + LocalSecretName(s.Name, k) + "}"
		envNames = append(envNames, k)
	}
	sort.Strings(envNames)
	var command string
	var args []string
	switch kind {
	case KindCommand:
		command, args = cmd, s.Args
	case KindNPM:
		command, args = "npx", append([]string{"-y", cmd}, s.Args...)
	case KindPip:
		command, args = "uvx", append([]string{cmd}, s.Args...)
	case KindImage:
		// The container sees only the env names passed with -e.
		command, args = "docker", []string{"run", "-i", "--rm"}
		for _, k := range envNames {
			args = append(args, "-e", k)
		}
		args = append(append(args, cmd), s.Args...)
	default:
		return nil, fmt.Errorf("unknown kind %q (use command, npm, pip or image)", kind)
	}
	if s.Share != "" && s.Share != ShareUser {
		return nil, fmt.Errorf(`share can only be "user"`)
	}
	e := map[string]any{"command": command}
	if s.Share != "" {
		e["share"] = s.Share
	}
	if len(args) > 0 {
		e["args"] = append([]string(nil), args...)
	}
	if len(env) > 0 {
		e["env"] = env
	}
	return e, nil
}

// SplitCommandLine splits a typed command line into words: spaces
// separate, '…' and "…" quote, \ escapes one character.
func SplitCommandLine(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inWord, quote, esc := false, rune(0), false
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\' && quote != '\'':
			esc, inWord = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 || esc {
		return nil, errors.New("a quote is not closed")
	}
	if inWord {
		out = append(out, cur.String())
	}
	if len(out) == 0 {
		return nil, errors.New("needs a command")
	}
	return out, nil
}

// SuggestName is an entry name for a package, image, program path or
// URL ("@scope/server-filesystem" → "filesystem").
func SuggestName(s string) string {
	s = strings.TrimSpace(s)
	if u, err := url.Parse(s); err == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http") {
		s = strings.ReplaceAll(u.Hostname(), ".", "-")
	} else {
		if i := strings.IndexByte(s, '@'); i > 0 {
			s = s[:i] // image digest / npm version
		}
		s = path.Base(s)
		if i := strings.IndexByte(s, ':'); i > 0 {
			s = s[:i] // image tag
		}
		for _, cut := range []string{"mcp-server-", "server-", "mcp_server_"} {
			s = strings.TrimPrefix(s, cut)
		}
		for _, cut := range []string{"-mcp-server", "-mcp", "_mcp"} {
			s = strings.TrimSuffix(s, cut)
		}
	}
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), "-_")
	if len(name) > 30 {
		name = strings.Trim(name[:30], "-_")
	}
	if name == "" {
		return "server"
	}
	return name
}

// ParseInstall reads install text (DI-5): a server URL, or an
// mcpServers block (ParseImport: literal tokens move to Secrets).
func ParseInstall(text string) ([]Imported, []string, error) {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "{") || strings.HasPrefix(t, "\"") {
		return ParseImport(t)
	}
	u, err := url.Parse(t)
	if err != nil || u.Scheme == "" {
		return nil, nil, errors.New("give a server URL (https://…) or an mcpServers block (JSON)")
	}
	if err := checkURL(t); err != nil {
		return nil, nil, err
	}
	return []Imported{{Name: SuggestName(t), Entry: map[string]any{"type": TypeHTTP, "url": t}, Secrets: map[string]string{}}}, nil, nil
}
