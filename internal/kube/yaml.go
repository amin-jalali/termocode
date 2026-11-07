package kube

import (
	"context"
	"strings"
)

// masked stands for a Secret value in the YAML view.
const masked = "'***'"

// lastApplied is the annotation `kubectl apply` writes: a full copy of the
// object, so on a Secret it holds the data too.
const lastApplied = "kubectl.kubernetes.io/last-applied-configuration"

// YAML is `kubectl get <kind> <name> -o yaml` as text (read only). A
// Secret's values are masked before the text leaves this package.
func (c Client) YAML(ctx context.Context, kind, name string) (string, error) {
	out, err := c.capture(ctx, append([]string{"get", kind, name, "-o", "yaml"}, c.scope(true)...)...)
	if err != nil {
		return "", err
	}
	return MaskSecretYAML(kind, out), nil
}

// isSecret reports whether kind (as typed, or the object's own kind line)
// is a Secret.
func isSecret(kind, text string) bool {
	switch strings.ToLower(kind) {
	case "secret", "secrets", "secret.v1", "secrets.v1":
		return true
	}
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimRight(l, " \r") == "kind: Secret" {
			return true
		}
	}
	return false
}

// MaskSecretYAML hides every value of a Secret's data and stringData and
// its last-applied annotation; key names and the rest stay. Other kinds
// come back unchanged. It reads kubectl's own YAML layout line by line:
// a value may span more lines (a block scalar), so lines deeper than a
// masked key go too.
func MaskSecretYAML(kind, text string) string {
	if !isSecret(kind, text) {
		return text
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	inData := false // inside a top-level data: or stringData: block
	skip := -1      // drop lines deeper than this indent (a masked value's rest)
	for _, l := range lines {
		trimmed := strings.TrimLeft(l, " ")
		indent := len(l) - len(trimmed)
		blank := strings.TrimSpace(trimmed) == ""
		if skip >= 0 {
			if blank || indent > skip {
				continue
			}
			skip = -1
		}
		if inData && !blank && indent == 0 {
			inData = false
		}
		key, _, _ := strings.Cut(trimmed, ":")
		switch {
		case inData && !blank:
			out = append(out, l[:indent]+key+": "+masked)
			skip = indent
		case indent == 0 && (key == "data" || key == "stringData"):
			out = append(out, key+":")
			inData = true
		case key == lastApplied:
			out = append(out, l[:indent]+key+": "+masked)
			skip = indent
		default:
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
