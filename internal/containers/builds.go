package containers

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
)

// BuildRecord is one row of the build history (docker buildx history ls).
type BuildRecord struct {
	Ref      string
	Name     string
	Status   string
	Created  time.Time
	Duration string
	Steps    int // total steps
	Done     int // completed steps
	Cached   int // steps taken from the cache
	Error    string
}

// Failed reports whether the build ended with an error.
func (b BuildRecord) Failed() bool {
	s := strings.ToLower(b.Status)
	return b.Error != "" || s == "error" || s == "failed" || s == "canceled"
}

// Builder is one buildx builder (docker buildx ls).
type Builder struct {
	Name      string
	Driver    string
	Current   bool
	Status    string
	Platforms []string
}

// BuildCacheUsage is the build cache row of `system df`.
type BuildCacheUsage struct {
	Count       int
	Active      int
	Size        string
	Reclaimable string
}

// parseObjects reads JSON lines or one JSON array of objects.
func parseObjects(data []byte, fn func(map[string]json.RawMessage) error) error {
	text := strings.TrimSpace(string(data))
	if !strings.HasPrefix(text, "[") {
		return parseLines(data, fn)
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &list); err != nil {
		return err
	}
	for _, raw := range list {
		if raw == nil {
			continue
		}
		if err := fn(raw); err != nil {
			return err
		}
	}
	return nil
}

// ParseBuildHistory parses `docker buildx history ls --format json`.
func ParseBuildHistory(data []byte) ([]BuildRecord, error) {
	var out []BuildRecord
	err := parseObjects(data, func(raw map[string]json.RawMessage) error {
		out = append(out, BuildRecord{
			Ref:      firstString(raw, "ref", "Ref", "ID"),
			Name:     firstString(raw, "name", "Name"),
			Status:   firstString(raw, "status", "Status"),
			Created:  parseTime(firstString(raw, "created_at", "CreatedAt", "createdAt")),
			Duration: firstString(raw, "duration", "Duration"),
			Steps:    firstInt(raw, "total_steps", "TotalSteps"),
			Done:     firstInt(raw, "completed_steps", "CompletedSteps"),
			Cached:   firstInt(raw, "cached_steps", "CachedSteps"),
			Error:    firstString(raw, "error", "Error"),
		})
		return nil
	})
	return out, err
}

// ParseBuilders parses `docker buildx ls --format json`.
func ParseBuilders(data []byte) ([]Builder, error) {
	var out []Builder
	err := parseObjects(data, func(raw map[string]json.RawMessage) error {
		b := Builder{
			Name:    firstString(raw, "Name", "name"),
			Driver:  firstString(raw, "Driver", "driver"),
			Current: truthy(firstString(raw, "Current", "current")),
		}
		var nodes []map[string]json.RawMessage
		if v, ok := raw["Nodes"]; ok {
			_ = json.Unmarshal(v, &nodes)
		}
		seen := map[string]bool{}
		for _, n := range nodes {
			if b.Status == "" {
				b.Status = firstString(n, "Status", "status")
			}
			for _, p := range stringList(n["Platforms"]) {
				if !seen[p] {
					seen[p] = true
					b.Platforms = append(b.Platforms, p)
				}
			}
		}
		out = append(out, b)
		return nil
	})
	return out, err
}

// ParseBuildCacheUsage picks the "Build Cache" row of
// `system df --format {{json .}}`.
func ParseBuildCacheUsage(data []byte) (BuildCacheUsage, error) {
	var u BuildCacheUsage
	found := false
	err := parseLines(data, func(raw map[string]json.RawMessage) error {
		if !strings.EqualFold(firstString(raw, "Type"), "Build Cache") {
			return nil
		}
		found = true
		u.Count, _ = strconv.Atoi(firstString(raw, "TotalCount"))
		u.Active, _ = strconv.Atoi(firstString(raw, "Active"))
		u.Size = firstString(raw, "Size")
		u.Reclaimable = firstString(raw, "Reclaimable")
		return nil
	})
	if err != nil {
		return u, err
	}
	if !found {
		return u, errors.New("the engine reports no build cache")
	}
	return u, nil
}

// SupportsBuildHistory reports whether the engine has buildx history and
// builders (docker only; podman builds through buildah).
func (e *Engine) SupportsBuildHistory() bool { return e.Kind != "podman" }

// BuildHistoryArgs is the argv (without the binary) of the history list.
func (e *Engine) BuildHistoryArgs() []string {
	return []string{"buildx", "history", "ls", "--format", "json"}
}

// BuildersArgs is the argv (without the binary) of the builders list.
func (e *Engine) BuildersArgs() []string { return []string{"buildx", "ls", "--format", "json"} }

// BuildCachePruneArgs is the argv of the cache prune. It deletes data: the
// UI asks with a Cancel-first card (showing ParseBuildCacheUsage) first.
func (e *Engine) BuildCachePruneArgs(all bool) []string {
	if e.Kind == "podman" {
		return []string{"image", "prune", "--build-cache", "--force"}
	}
	args := []string{"builder", "prune", "--force"}
	if all {
		args = append(args, "--all")
	}
	return args
}

var errNoBuildHistory = errors.New("build history needs docker buildx")

// BuildHistory lists recent builds.
func (e *Engine) BuildHistory(ctx context.Context) ([]BuildRecord, error) {
	if !e.SupportsBuildHistory() {
		return nil, errNoBuildHistory
	}
	return parseList(ctx, e, e.BuildHistoryArgs(), ParseBuildHistory)
}

// Builders lists buildx builders.
func (e *Engine) Builders(ctx context.Context) ([]Builder, error) {
	if !e.SupportsBuildHistory() {
		return nil, errNoBuildHistory
	}
	return parseList(ctx, e, e.BuildersArgs(), ParseBuilders)
}

// BuildCache reads the build cache usage.
func (e *Engine) BuildCache(ctx context.Context) (BuildCacheUsage, error) {
	out, err := e.list(ctx, "system", "df", "--format", "{{json .}}")
	if err != nil {
		return BuildCacheUsage{}, err
	}
	return ParseBuildCacheUsage([]byte(out))
}

// PruneBuildCache removes build cache (after the user's card).
func (e *Engine) PruneBuildCache(ctx context.Context, all bool) error {
	return e.action(ctx, 5*time.Minute, e.BuildCachePruneArgs(all)...)
}

// BuildDetailCommand is a visible job showing one build's detail.
func (e *Engine) BuildDetailCommand(ref string) (services.Command, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "-") {
		return services.Command{}, errors.New("invalid build reference")
	}
	return services.Command{Label: "build: " + ref, Argv: []string{e.Binary, "buildx", "history", "inspect", ref}}, nil
}

// BuildCheckCommand runs the Dockerfile build checks (no image is built)
// in dir; file is the Dockerfile name, empty for the default.
func (e *Engine) BuildCheckCommand(dir, file string) services.Command {
	argv := []string{e.Binary, "build", "--check"}
	if file != "" {
		argv = append(argv, "-f", file)
	}
	argv = append(argv, ".")
	return services.Command{Label: "build check", Argv: argv, Dir: dir}
}
