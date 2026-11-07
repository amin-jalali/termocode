package kube

import (
	"context"
	"errors"
	"strings"
	"time"
)

// applyTimeout bounds `diff` and `apply` of one file (the server runs a
// dry-run or the real apply for every object in it).
const applyTimeout = 60 * time.Second

// FileDiff is what `kubectl diff -f` found: Changed is false when the
// cluster already matches the file.
type FileDiff struct {
	Text    string
	Changed bool
}

func checkFilePath(path string) error {
	if path == "" || strings.HasPrefix(path, "-") {
		return errors.New("kube: a YAML file path is needed")
	}
	return nil
}

// DiffFile is `kubectl diff -f <path>`: a server-side dry-run of the file
// shown as a diff. It never changes the cluster. kubectl exits 1 when
// there are changes; only higher codes are errors.
func (c Client) DiffFile(parent context.Context, path string) (FileDiff, error) {
	if err := checkFilePath(path); err != nil {
		return FileDiff{}, err
	}
	if c.Binary == "" {
		return FileDiff{}, &Error{Kind: ErrNoKubectl}
	}
	d := c.Timeout
	if d < applyTimeout {
		d = applyTimeout
	}
	ctx, cancel := context.WithTimeout(parent, d)
	defer cancel()
	argv := append([]string{c.Binary, "diff"}, c.scope(true)...)
	argv = append(argv, "-f", path)
	out, stderr, code, err := c.exec().Capture(ctx, "", argv...)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil || code > 1 {
		return FileDiff{}, ErrorLine(stderr, err)
	}
	return FileDiff{Text: out, Changed: code == 1}, nil
}

// ApplyFile is `kubectl apply -f <path>`: a write. The app shows DiffFile
// and asks the user first; the AI never runs it.
func (c Client) ApplyFile(ctx context.Context, path string) (string, error) {
	if err := checkFilePath(path); err != nil {
		return "", err
	}
	if c.Timeout < applyTimeout {
		c.Timeout = applyTimeout
	}
	return c.capture(ctx, append(append([]string{"apply"}, c.scope(true)...), "-f", path)...)
}
