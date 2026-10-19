package containers

import "context"

func (e *Engine) ListContainers(ctx context.Context) ([]Container, error) {
	return parseList(ctx, e, []string{"ps", "--all", "--no-trunc", "--format", "{{json .}}"}, ParseContainers)
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
func (e *Engine) ListContexts(ctx context.Context) ([]Context, error) {
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
