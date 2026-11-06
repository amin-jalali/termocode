package run

import (
	"context"
	"fmt"
	"sort"
)

// ExprFilter is the default Filter: it keeps the items for which the
// step's when is true, drops repeats of dedupe, sorts by sort (numbers
// before text, missing last) and keeps the first take.
type ExprFilter struct{}

// Filter implements Filter.
func (ExprFilter) Filter(ctx context.Context, c FilterCall) ([]Item, error) {
	fs := c.Step.Filter
	out := make([]Item, 0, len(c.Items))
	seen := map[string]bool{}
	for _, it := range c.Items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sc := c.Scope(it)
		if c.Step.When != "" {
			ok, err := c.Eval.Test(c.Step.When, sc)
			if err != nil {
				return nil, fmt.Errorf("when: %w", err)
			}
			if !ok {
				continue
			}
		}
		if fs != nil && fs.Dedupe != "" {
			v, err := c.Eval.Eval(fs.Dedupe, sc)
			if err != nil {
				return nil, fmt.Errorf("dedupe: %w", err)
			}
			k := fmt.Sprintf("%T:%v", v, v)
			if seen[k] {
				continue
			}
			seen[k] = true
		}
		out = append(out, it)
	}
	if fs != nil && fs.Sort != "" {
		keys := make([]any, len(out))
		for i, it := range out {
			v, err := c.Eval.Eval(fs.Sort, c.Scope(it))
			if err != nil {
				return nil, fmt.Errorf("sort: %w", err)
			}
			keys[i] = v
		}
		idx := make([]int, len(out))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool { return less(keys[idx[a]], keys[idx[b]]) })
		sorted := make([]Item, len(out))
		for i, j := range idx {
			sorted[i] = out[j]
		}
		out = sorted
	}
	if fs != nil && fs.Take > 0 && len(out) > fs.Take {
		out = out[:fs.Take]
	}
	return out, nil
}

// less orders sort keys: numbers (small first), then text, then the
// rest (missing last).
func less(a, b any) bool {
	ra, rb := sortRank(a), sortRank(b)
	if ra != rb {
		return ra < rb
	}
	switch x := a.(type) {
	case float64:
		return x < b.(float64)
	case string:
		return x < b.(string)
	}
	return false
}

func sortRank(v any) int {
	switch v.(type) {
	case float64:
		return 0
	case string:
		return 1
	case nil:
		return 3
	}
	return 2
}
