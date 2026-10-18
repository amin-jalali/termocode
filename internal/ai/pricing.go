package ai

import (
	"sort"
	"strings"
)

// Price is what a model costs in US dollars per million tokens.
type Price struct {
	In         float64 `json:"in"`
	Out        float64 `json:"out"`
	CacheRead  float64 `json:"cache_read,omitempty"`  // 0: same as In
	CacheWrite float64 `json:"cache_write,omitempty"` // 0: same as In
}

// PricesDate is the day the built-in price and window tables were
// checked. Prices change: config.json "ai_prices" overrides or adds
// models, "ai_context_windows" the windows. Nothing is looked up online.
const PricesDate = "2026-10-18"

// builtinPrices maps a model id prefix to its list price (first-party
// API rates on PricesDate). The longest matching prefix wins, so a dated
// id (claude-haiku-4-5-20251001) finds its model.
var builtinPrices = map[string]Price{
	// Anthropic. Cache write is the 5-minute rate (1.25 × input).
	"claude-opus-5-5":   {In: 4, Out: 20, CacheRead: 0.20, CacheWrite: 5},
	"claude-opus-5":     {In: 5, Out: 25, CacheRead: 0.50, CacheWrite: 6.25},
	"claude-opus-4-8":   {In: 5, Out: 25, CacheRead: 0.50, CacheWrite: 6.25},
	"claude-opus-4-7":   {In: 5, Out: 25, CacheRead: 0.50, CacheWrite: 6.25},
	"claude-opus-4-6":   {In: 5, Out: 25, CacheRead: 0.50, CacheWrite: 6.25},
	"claude-opus-4-5":   {In: 5, Out: 25, CacheRead: 0.50, CacheWrite: 6.25},
	"claude-opus-4":     {In: 15, Out: 75, CacheRead: 1.50, CacheWrite: 18.75}, // 4 and 4.1
	"claude-sonnet-5-5": {In: 2, Out: 10, CacheRead: 0.20, CacheWrite: 2.50},
	"claude-sonnet-5":   {In: 2, Out: 10, CacheRead: 0.20, CacheWrite: 2.50},
	"claude-sonnet-4":   {In: 3, Out: 15, CacheRead: 0.30, CacheWrite: 3.75}, // 4, 4.5, 4.6
	"claude-haiku-5-5":  {In: 0.10, Out: 0.50, CacheRead: 0.01, CacheWrite: 0.125},
	"claude-haiku-4-5":  {In: 1, Out: 5, CacheRead: 0.10, CacheWrite: 1.25},
	"claude-3-5-haiku":  {In: 0.80, Out: 4, CacheRead: 0.08, CacheWrite: 1},
	// OpenAI. Cached input is CacheRead; there is no cache write charge.
	"gpt-5":        {In: 1.25, Out: 10, CacheRead: 0.125, CacheWrite: 1.25},
	"gpt-5-mini":   {In: 0.25, Out: 2, CacheRead: 0.025, CacheWrite: 0.25},
	"gpt-5-nano":   {In: 0.05, Out: 0.40, CacheRead: 0.005, CacheWrite: 0.05},
	"gpt-4.1":      {In: 2, Out: 8, CacheRead: 0.50, CacheWrite: 2},
	"gpt-4.1-mini": {In: 0.40, Out: 1.60, CacheRead: 0.10, CacheWrite: 0.40},
	"gpt-4.1-nano": {In: 0.10, Out: 0.40, CacheRead: 0.025, CacheWrite: 0.10},
	"gpt-4o":       {In: 2.50, Out: 10, CacheRead: 1.25, CacheWrite: 2.50},
	"gpt-4o-mini":  {In: 0.15, Out: 0.60, CacheRead: 0.075, CacheWrite: 0.15},
	"o3":           {In: 2, Out: 8, CacheRead: 0.50, CacheWrite: 2},
	"o4-mini":      {In: 1.10, Out: 4.40, CacheRead: 0.275, CacheWrite: 1.10},
}

// builtinWindows maps a model id prefix to its context window in tokens.
var builtinWindows = map[string]int{
	"claude-opus-5":     1_000_000, // 5 and 5.5
	"claude-opus-4-8":   1_000_000,
	"claude-opus-4-7":   1_000_000,
	"claude-opus-4-6":   1_000_000,
	"claude-opus-4":     200_000,
	"claude-sonnet-5":   1_000_000,
	"claude-sonnet-4-6": 1_000_000,
	"claude-sonnet-4":   200_000,
	"claude-haiku-5-5":  1_000_000,
	"claude-haiku-4-5":  200_000,
	"claude-3-5-haiku":  200_000,
	"gpt-5":             400_000,
	"gpt-4.1":           1_047_576,
	"gpt-4o":            128_000,
	"o3":                200_000,
	"o4-mini":           200_000,
}

// Pricing is the built-in price table plus the user's overrides.
type Pricing struct{ user map[string]Price }

// NewPricing puts overrides (config.json "ai_prices") over the built-in
// table: a user entry wins over every built-in one, even a longer one.
func NewPricing(overrides map[string]Price) Pricing {
	u := make(map[string]Price, len(overrides))
	for k, v := range overrides {
		if k = strings.TrimSpace(k); k != "" {
			u[k] = v
		}
	}
	return Pricing{user: u}
}

// Lookup returns the price of model by the longest matching prefix, the
// user's entries first. A vendor prefix (OpenRouter's "anthropic/…") is
// tried with and without.
func (p Pricing) Lookup(model string) (Price, bool) {
	pr, ok := lookupPrefix(p.user, model)
	if !ok {
		pr, ok = lookupPrefix(builtinPrices, model)
	}
	if !ok {
		return Price{}, false
	}
	if pr.CacheRead == 0 {
		pr.CacheRead = pr.In
	}
	if pr.CacheWrite == 0 {
		pr.CacheWrite = pr.In
	}
	return pr, true
}

// Cost is the price in US dollars of u, sent through a provider of kind.
// Local servers (Ollama, LM Studio) cost 0; a Claude sign-in is a
// subscription, not billed per token, and has no cost; an unknown model
// has no cost (ok = false), never a guess.
func (p Pricing) Cost(kind string, u Usage) (float64, bool) {
	switch kind {
	case KindOllama, KindLMStudio:
		return 0, true
	case KindClaudeOAuth:
		return 0, false
	}
	pr, ok := p.Lookup(u.Model)
	if !ok {
		return 0, false
	}
	usd := float64(u.Input)*pr.In + float64(u.Output)*pr.Out +
		float64(u.CacheRead)*pr.CacheRead + float64(u.CacheWrite)*pr.CacheWrite
	return usd / 1e6, true
}

// ContextWindows is the built-in window table plus the user's overrides.
type ContextWindows struct{ user map[string]int }

// NewContextWindows puts overrides (config.json "ai_context_windows")
// over the built-in table, like NewPricing. Values ≤ 0 are ignored.
func NewContextWindows(overrides map[string]int) ContextWindows {
	u := make(map[string]int, len(overrides))
	for k, v := range overrides {
		if k = strings.TrimSpace(k); k != "" && v > 0 {
			u[k] = v
		}
	}
	return ContextWindows{user: u}
}

// Lookup returns the context window of model (0, false when unknown).
func (w ContextWindows) Lookup(model string) (int, bool) {
	if n, ok := lookupPrefix(w.user, model); ok {
		return n, true
	}
	return lookupPrefix(builtinWindows, model)
}

// ContextPercent is how full the window got: u's biggest prompt over
// window, in percent. No window → ok = false.
func ContextPercent(u Usage, window int) (float64, bool) {
	if window <= 0 {
		return 0, false
	}
	return float64(u.PeakContext) * 100 / float64(window), true
}

// lookupPrefix finds model in t by the longest key that is a prefix of
// it, first as given, then without a "vendor/" part.
func lookupPrefix[V any](t map[string]V, model string) (V, bool) {
	var zero V
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return zero, false
	}
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	try := []string{model}
	if i := strings.LastIndexByte(model, '/'); i >= 0 {
		try = append(try, model[i+1:])
	}
	for _, m := range try {
		for _, k := range keys {
			if strings.HasPrefix(m, strings.ToLower(k)) {
				return t[k], true
			}
		}
	}
	return zero, false
}
