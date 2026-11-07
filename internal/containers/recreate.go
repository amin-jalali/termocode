package containers

import (
	"context"
	"encoding/json"
	"errors"
)

// InspectRaw is the `inspect <id>` JSON text (for RunOptionsFromInspect).
func (e *Engine) InspectRaw(ctx context.Context, id string) ([]byte, error) {
	if !runWord(id) {
		return nil, errors.New("inspect: bad container")
	}
	out, err := e.list(ctx, "inspect", id)
	return []byte(out), err
}

// RecreatePlan is "Recreate container": pull the image again, remove the
// old container (force: it may run), then run one with the same settings.
// The env values come back for the job's environment; argv names them only.
func (e *Engine) RecreatePlan(id string, o RunOptions) ([][]string, map[string]string, error) {
	if !runWord(id) {
		return nil, nil, errors.New("recreate: bad container")
	}
	run, err := e.RunOptionsCommand(o)
	if err != nil {
		return nil, nil, err
	}
	return [][]string{{e.Binary, "pull", o.Image}, {e.Binary, "rm", "-f", id}, run.Argv}, run.Env, nil
}

// ImageRepoDigests reads the RepoDigests ("repo@sha256:…") of the first
// object of `inspect <image>` output; nil when there are none.
func ImageRepoDigests(b []byte) []string {
	var list []struct{ RepoDigests []string }
	if json.Unmarshal(b, &list) != nil || len(list) == 0 {
		return nil
	}
	return list[0].RepoDigests
}
