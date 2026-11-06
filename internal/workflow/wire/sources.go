package wire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/source"
)

// Sources is the run.Source over the source adapters: it maps a file's
// SourceSpec (plan §3.3) to the input of its kind.
type Sources struct {
	Src *source.Sources
	// Eval fills the key template per item (nil: the source's own key).
	Eval run.Evaluator
}

// Read runs one source step.
func (s *Sources) Read(ctx context.Context, c run.SourceCall) (run.SourceResult, error) {
	if c.Step == nil || c.Step.Source == nil {
		return run.SourceResult{}, run.Permanent(errors.New("the step has no source"))
	}
	if s.Src == nil {
		return run.SourceResult{}, run.Permanent(errors.New("no sources are set up"))
	}
	sp := c.Step.Source
	shape := source.Shape{Items: sp.Items, MaxItems: sp.MaxItems}
	keyTmpl := sp.Key
	var res source.Result
	var err error
	switch sp.From {
	case "mcp":
		res, err = s.Src.MCP(ctx, source.MCPInput{Server: sp.Server, Tool: sp.Tool, Args: c.Args,
			Confirmed: !c.ReadOnly, Timeout: c.Step.Timeout, Shape: shape})
	case "github":
		in := source.GitHubInput{Kind: sp.What, Timeout: c.Step.Timeout, Shape: shape}
		in.Repo, _ = c.Args["repo"].(string)
		in.State, _ = c.Args["state"].(string)
		in.Labels = stringList(c.Args["labels"])
		if l, ok := c.Args["label"].(string); ok && l != "" {
			in.Labels = append(in.Labels, l)
		}
		in.Number = intOf(c.Args["number"])
		if in.Number == 0 {
			in.Number = intOf(c.Args["pr"])
		}
		res, err = s.Src.GitHub(ctx, in)
	case "shell":
		if len(sp.Env) > 0 {
			return run.SourceResult{}, run.Permanent(errors.New("shell: env is not supported for a source yet"))
		}
		res, err = s.Src.Shell(ctx, source.ShellInput{Argv: sp.Argv, Cwd: sp.Cwd, Timeout: c.Step.Timeout, Parse: sp.Parse, Shape: shape})
	case "files":
		var since time.Time
		if t := strings.TrimSpace(c.ChangedSince); t != "" {
			if since, err = time.Parse(time.RFC3339, t); err != nil {
				return run.SourceResult{}, run.Permanent(fmt.Errorf("changed_since %q is not a time like 2026-11-06T09:00:00Z", t))
			}
		}
		if keyTmpl == "" {
			keyTmpl = "{{item.path}}"
		}
		res, err = s.Src.Files(ctx, source.FilesInput{Glob: sp.Glob, ChangedSince: since, Content: true, MaxItems: sp.MaxItems})
	case "http":
		res, err = s.Src.HTTP(ctx, source.HTTPInput{RequestFile: sp.Request, Headers: sp.Headers, Timeout: c.Step.Timeout, Shape: shape})
	case "db":
		res, err = s.Src.DB(ctx, source.DBInput{Conn: sp.Connection, SQL: sp.SQL, Timeout: c.Step.Timeout, Shape: shape})
	default:
		return run.SourceResult{}, run.Permanent(fmt.Errorf("source kind %q is not known", sp.From))
	}
	if err != nil {
		return run.SourceResult{}, err
	}
	out := run.SourceResult{Truncated: res.Truncated, Total: res.Total, Items: make([]run.Item, 0, len(res.Items))}
	for _, it := range res.Items {
		var data any
		if err := json.Unmarshal(it.Data, &data); err != nil {
			return run.SourceResult{}, fmt.Errorf("item %s: %w", it.Key, err)
		}
		key := it.Key
		if keyTmpl != "" && s.Eval != nil {
			k, err := s.Eval.Render(keyTmpl, map[string]any{"item": data})
			if err != nil {
				return run.SourceResult{}, run.Permanent(fmt.Errorf("key: %w", err))
			}
			if k = strings.TrimSpace(k); k != "" {
				key = k
			}
		}
		out.Items = append(out.Items, run.Item{Key: key, Data: data})
	}
	return out, nil
}

func stringList(v any) []string {
	var out []string
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	case string:
		for _, s := range strings.Split(x, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func intOf(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	}
	return 0
}
