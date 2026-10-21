package containers

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
)

// ── What the AI tools may send and what they may read (CP-9) ─────────────
//
// Every id, name and image reference the model gives goes into an argv
// list (never a shell line) and must pass these strict patterns first: no
// leading "-" (it would be read as a flag), no spaces, no shell or path
// characters. Inspect output passes MaskInspect before it reaches the
// model: env NAMES stay, every env VALUE is masked, auth fields are
// masked, and labels outside the compose / OCI keys that look secret are
// masked.

const redacted = "[REDACTED]"

var (
	imageRefRe = regexp.MustCompile(`^(?:[a-z0-9]+(?:[.-][a-z0-9]+)*(?::[0-9]{1,5})?/)?` +
		`[a-z0-9]+(?:[._-]+[a-z0-9]+)*(?:/[a-z0-9]+(?:[._-]+[a-z0-9]+)*)*` +
		`(?::[A-Za-z0-9_][A-Za-z0-9._-]{0,127})?(?:@sha256:[a-f0-9]{64})?$`)
	nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
)

// ValidImageRef checks an image reference the model gave (pull).
func ValidImageRef(ref string) error {
	if ref == "" {
		return errors.New("give an image reference like nginx:1.27")
	}
	if strings.HasPrefix(ref, "-") {
		return errors.New("an image reference cannot start with \"-\"")
	}
	if len(ref) > 255 || !imageRefRe.MatchString(ref) {
		return fmt.Errorf("%q is not an image reference (lower-case repo[:tag][@sha256:digest], e.g. ghcr.io/acme/api:1.4)", clip(ref))
	}
	return nil
}

// ValidName checks a container id or name, a compose project or
// service, a volume or a network name the model gave.
func ValidName(s string) error {
	if s == "" {
		return errors.New("give an id or a name")
	}
	if strings.HasPrefix(s, "-") {
		return errors.New("an id or name cannot start with \"-\"")
	}
	if !nameRe.MatchString(s) {
		return fmt.Errorf("%q is not a valid id or name (letters, digits, _ . -)", clip(s))
	}
	return nil
}

func clip(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}

// safeLabelPrefixes are the label keys a container listing may show the
// model: other labels may carry tokens.
var safeLabelPrefixes = []string{"com.docker.compose.", "org.opencontainers.image."}

func safeLabel(k string) bool {
	for _, p := range safeLabelPrefixes {
		if strings.HasPrefix(k, p) {
			return true
		}
	}
	return false
}

// SafeLabels keeps only the compose and OCI labels.
func SafeLabels(l map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range l {
		if safeLabel(k) {
			out[k] = v
		}
	}
	return out
}

// secretField names inspect fields that hold credentials.
func secretField(k string) bool {
	k = strings.ToLower(k)
	for _, w := range []string{"auth", "password", "secret", "token", "credential"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}

// MaskInspect returns a copy of a decoded inspect value with every env
// value, auth field and secret-looking label or argument masked, and how
// many values it masked.
func MaskInspect(v any) (any, int) {
	n := 0
	return maskValue(v, &n), n
}

func maskValue(v any, n *int) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			switch {
			case k == "Env":
				out[k] = maskEnv(val, n)
			case k == "Labels":
				out[k] = maskLabels(val, n)
			case secretField(k):
				if s, ok := val.(string); ok {
					if s != "" {
						*n++
						s = redacted
					}
					out[k] = s
				} else {
					out[k] = maskValue(val, n)
				}
			default:
				out[k] = maskValue(val, n)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = maskValue(val, n)
		}
		return out
	case string:
		s, c := services.Redact(x)
		*n += c
		return s
	}
	return v
}

// maskEnv keeps NAME and masks the value of every NAME=value.
func maskEnv(v any, n *int) any {
	list, ok := v.([]any)
	if !ok {
		return maskValue(v, n)
	}
	out := make([]any, len(list))
	for i, item := range list {
		s, ok := item.(string)
		if !ok {
			out[i] = maskValue(item, n)
			continue
		}
		name, val, has := strings.Cut(s, "=")
		if has && val != "" {
			*n++
			s = name + "=" + redacted
		}
		out[i] = s
	}
	return out
}

// maskLabels keeps compose / OCI labels (URL passwords masked) and masks
// other labels whose key or value looks secret.
func maskLabels(v any, n *int) any {
	l, ok := v.(map[string]any)
	if !ok {
		return maskValue(v, n)
	}
	out := make(map[string]any, len(l))
	for k, val := range l {
		s, isStr := val.(string)
		switch {
		case !isStr:
			out[k] = maskValue(val, n)
		case !safeLabel(k) && services.IsSecretKey(k) && s != "":
			*n++
			out[k] = redacted
		default:
			r, c := services.Redact(s)
			*n += c
			out[k] = r
		}
	}
	return out
}

// Logs captures the last lines of a container's logs (stdout and stderr)
// with timestamps. since is an engine duration like 10m ("" = all).
func (e *Engine) Logs(ctx context.Context, id string, tail int, since string) (string, error) {
	args := []string{"logs", "--tail", strconv.Itoa(tail), "--timestamps"}
	if since != "" {
		args = append(args, "--since", since)
	}
	args = append(args, id)
	out, stderr, code, err := e.captureTimeout(ctx, 15*time.Second, args...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		p := services.ProblemFromStderr(stderr)
		if p.Kind == services.ProblemNone {
			return "", fmt.Errorf("engine command exited %d: %s", code, strings.TrimSpace(firstLine(stderr)))
		}
		return "", errors.New(p.Message())
	}
	text := strings.TrimRight(out, "\n")
	if s := strings.TrimRight(stderr, "\n"); s != "" {
		if text != "" {
			text += "\n"
		}
		text += s
	}
	return text, nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

// InspectType is `inspect [--type kind] id` (kind: container, image,
// volume, network; "" lets the engine choose).
func (e *Engine) InspectType(ctx context.Context, kind, id string) (any, error) {
	args := []string{"inspect"}
	if kind != "" {
		args = append(args, "--type", kind)
	}
	out, err := e.list(ctx, append(args, id)...)
	if err != nil {
		return nil, err
	}
	return ParseInspect([]byte(out))
}
