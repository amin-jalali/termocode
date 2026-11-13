package search

import (
	"context"
	"path"
	"slices"
	"strings"
)

// Remote runs one command in a folder of the workspace host (a container
// workspace's Host Exec: `docker exec -w <dir> …`). services.Exec fits.
type Remote interface {
	Capture(ctx context.Context, dir string, argv ...string) (stdout, stderr string, code int, err error)
}

// runRemote is the search of a container workspace (opts.FS set): rg in
// the container, else grep for a literal query, names the files that may
// hold the text, and only those are read through opts.FS and matched by
// the built-in matcher (the same results as the walker, far fewer engine
// calls). No tool, or a tool that fails, walks every file (runFS).
func runRemote(ctx context.Context, dir, query string, opts Options, onFile func([]Result)) error {
	files, ok := remoteCandidates(ctx, dir, query, opts)
	if !ok {
		return runFS(ctx, opts.FS, dir, query, opts, onFile)
	}
	lm, err := newLineMatcher(query, opts)
	if err != nil {
		return err
	}
	globs := globFilter{include: opts.Include, exclude: opts.Exclude}
	for _, rel := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !remoteKeep(rel, globs) {
			continue
		}
		data, err := opts.FS.ReadFile(ctx, path.Join(dir, rel))
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		if len(data) > maxFallbackFileSize || isBinary(data) || !lm.mayMatch(data) {
			continue
		}
		if rs := searchLines(string(data), rel, lm, opts.Context); len(rs) > 0 {
			onFile(rs)
		}
	}
	return nil
}

// remoteKeep applies the walker's folder skips and the globs to rel.
func remoteKeep(rel string, globs globFilter) bool {
	parts := strings.Split(rel, "/")
	for i, p := range parts[:len(parts)-1] {
		if skipSearchDir(p) || globs.excluded(strings.Join(parts[:i+1], "/"), p) {
			return false
		}
	}
	return globs.allowFile(rel, parts[len(parts)-1])
}

// remoteCandidates lists the files under dir that may match (sorted,
// relative). ok=false: no usable tool, so walk.
func remoteCandidates(ctx context.Context, dir, query string, opts Options) ([]string, bool) {
	if opts.Remote == nil {
		return nil, false
	}
	var argv []string
	sep := "\n"
	switch {
	case slices.Contains(opts.RemoteTools, "rg"):
		argv = []string{"rg", "--files-with-matches", "--null", "--ignore-case", "--no-messages"}
		if !opts.Regex {
			argv = append(argv, "--fixed-strings")
		}
		argv = append(argv, "--", query)
		sep = "\x00"
	case slices.Contains(opts.RemoteTools, "grep") && !opts.Regex:
		argv = []string{"grep", "-rlFis", "-e", query, "."}
	default:
		return nil, false
	}
	out, _, code, err := opts.Remote.Capture(ctx, dir, argv...)
	switch {
	case err != nil:
		return nil, false
	case code == 1 && strings.TrimSpace(out) == "":
		return nil, true // no file holds it
	case code != 0 && code != 1:
		return nil, false // gone from the image, or another failure: walk
	}
	var files []string
	for _, f := range strings.Split(out, sep) {
		f = strings.TrimPrefix(strings.TrimSpace(f), "./")
		if f != "" && !strings.HasPrefix(f, "../") && !path.IsAbs(f) {
			files = append(files, path.Clean(f))
		}
	}
	slices.Sort(files)
	return slices.Compact(files), true
}
