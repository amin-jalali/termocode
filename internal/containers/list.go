package containers

import "context"

func (e *Engine) ListContainers(ctx context.Context) ([]Container, error) {
	return parseList(ctx, e, ListContainersArgs(false), ParseContainers)
}

// ListContainersSized is ListContainers with each container's disk size
// (`ps --size`, slower: the engine measures every writable layer).
func (e *Engine) ListContainersSized(ctx context.Context, sized bool) ([]Container, error) {
	return parseList(ctx, e, ListContainersArgs(sized), ParseContainers)
}

// ListContainersArgs is the engine argv of the container list.
func ListContainersArgs(sized bool) []string {
	if sized {
		return []string{"ps", "--all", "--no-trunc", "--size", "--format", "{{json .}}"}
	}
	return []string{"ps", "--all", "--no-trunc", "--format", "{{json .}}"}
}
func (e *Engine) ListImages(ctx context.Context) ([]Image, error) {
	return parseList(ctx, e, []string{"images", "--no-trunc", "--format", "{{json .}}"}, ParseImages)
}
func (e *Engine) ListNetworks(ctx context.Context) ([]Network, error) {
	return parseList(ctx, e, []string{"network", "ls", "--format", "{{json .}}"}, ParseNetworks)
}
func (e *Engine) ListVolumes(ctx context.Context) ([]Volume, error) {
	return parseList(ctx, e, []string{"volume", "ls", "--format", "{{json .}}"}, ParseVolumes)
}
func (e *Engine) ListDanglingVolumes(ctx context.Context) ([]Volume, error) {
	return parseList(ctx, e, []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{json .}}"}, ParseVolumes)
}
func (e *Engine) ContainersUsingVolume(ctx context.Context, name string) ([]Container, error) {
	return parseList(ctx, e, []string{"ps", "--all", "--filter", "volume=" + name, "--format", "{{json .}}"}, ParseContainers)
}

// ListContexts lists docker contexts, or podman connections (CP-12).
func (e *Engine) ListContexts(ctx context.Context) ([]Context, error) {
	if e.Kind == "podman" {
		return e.ListConnections(ctx)
	}
	return parseList(ctx, e, []string{"context", "ls", "--format", "{{json .}}"}, ParseContexts)
}
func (e *Engine) Inspect(ctx context.Context, kind, id string) (any, error) {
	out, err := e.list(ctx, "inspect", id)
	if err != nil {
		return nil, err
	}
	return ParseInspect([]byte(out))
}
func (e *Engine) Stats(ctx context.Context, ids ...string) ([]Stat, error) {
	args := append([]string{"stats", "--no-stream", "--format", "{{json .}}"}, ids...)
	return parseList(ctx, e, args, ParseStats)
}
func (e *Engine) History(ctx context.Context, ref string) ([]HistoryEntry, error) {
	return parseList(ctx, e, []string{"history", "--no-trunc", "--format", "{{json .}}", ref}, ParseHistory)
}
func (e *Engine) DiskUsage(ctx context.Context) (string, error) {
	return e.list(ctx, "system", "df", "-v")
}
