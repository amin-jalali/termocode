package kube

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// RolloutRevision is one row of `kubectl rollout history`.
type RolloutRevision struct {
	Number int
	Cause  string // "" when kubectl shows <none>
}

func checkName(what, s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("no " + what)
	}
	if strings.HasPrefix(s, "-") || strings.ContainsAny(s, " \t\n=") {
		return errors.New("not a valid " + what + ": " + s)
	}
	return nil
}

// RolloutStatus is `kubectl rollout status deployment/<name> --watch=false`
// (read only; it does not wait).
func (c Client) RolloutStatus(ctx context.Context, name string) (string, error) {
	if err := checkName("deployment name", name); err != nil {
		return "", err
	}
	out, err := c.capture(ctx, append([]string{"rollout", "status", "deployment/" + name, "--watch=false"}, c.scope(true)...)...)
	return strings.TrimSpace(out), err
}

// RolloutHistory is `kubectl rollout history deployment/<name>`, parsed
// (read only).
func (c Client) RolloutHistory(ctx context.Context, name string) ([]RolloutRevision, error) {
	if err := checkName("deployment name", name); err != nil {
		return nil, err
	}
	out, err := c.capture(ctx, append([]string{"rollout", "history", "deployment/" + name}, c.scope(true)...)...)
	if err != nil {
		return nil, err
	}
	return parseHistory(out), nil
}

func parseHistory(out string) []RolloutRevision {
	var revs []RolloutRevision
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		n, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		cause := strings.TrimSpace(strings.TrimSpace(line)[len(f[0]):])
		if cause == "<none>" {
			cause = ""
		}
		revs = append(revs, RolloutRevision{Number: n, Cause: cause})
	}
	return revs
}

// RolloutUndo is `kubectl rollout undo deployment/<name>` (revision 0 =
// the previous one). A write: the app asks first; the AI never runs it.
func (c Client) RolloutUndo(ctx context.Context, name string, revision int) error {
	if err := checkName("deployment name", name); err != nil {
		return err
	}
	if revision < 0 {
		return errors.New("not a valid revision")
	}
	if c.Timeout <= 0 || c.Timeout < restartTimeout {
		c.Timeout = restartTimeout
	}
	args := []string{"rollout", "undo", "deployment/" + name}
	if revision > 0 {
		args = append(args, "--to-revision="+strconv.Itoa(revision))
	}
	_, err := c.capture(ctx, append(args, c.scope(true)...)...)
	return err
}

// SetImage is `kubectl set image deployment/<name> <container>=<image>`.
// A write: the app asks first; the AI never runs it.
func (c Client) SetImage(ctx context.Context, name, container, image string) error {
	if err := checkName("deployment name", name); err != nil {
		return err
	}
	if err := checkName("container name", container); err != nil {
		return err
	}
	if err := checkName("image", image); err != nil {
		return err
	}
	if c.Timeout <= 0 || c.Timeout < restartTimeout {
		c.Timeout = restartTimeout
	}
	_, err := c.capture(ctx, append([]string{"set", "image", "deployment/" + name, container + "=" + image}, c.scope(true)...)...)
	return err
}
