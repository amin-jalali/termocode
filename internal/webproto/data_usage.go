package webproto

// AI usage for the phone (MOB-73, mobile plan 3.11.8 "usage"): this
// session's usage as the laptop's USAGE view and the usage_summary tool
// count it, per model and per agent step. Numbers and names only: no
// prompt, no key, no account id. A subscription step has no dollar cost.

// OpAIUsage answers this session's AI usage.
const OpAIUsage = "ai.usage"

// Caps of ai.usage.
const (
	MaxUsageLines = 50 // lines per section
	MaxUsageName  = 60 // runes of a model or agent name
)

// AIUsage answers ai.usage.
type AIUsage struct {
	Scope   string        `json:"scope"`    // "this session" …
	ByModel []AIUsageLine `json:"by_model"` // most calls first, as the laptop sorts them
	ByAgent []AIUsageLine `json:"by_agent"`
	Total   AIUsageLine   `json:"total"`
}

// AIUsageLine is one model's or agent step's usage.
type AIUsageLine struct {
	Name    string  `json:"name"`
	Calls   int     `json:"calls"`
	Input   int     `json:"input"`  // input tokens
	Output  int     `json:"output"` // output tokens
	CostUSD float64 `json:"cost_usd,omitempty"`
	Priced  bool    `json:"priced,omitempty"`                                              // CostUSD counts a known price
	Billing string  `json:"billing,omitempty"` // per token, subscription, mixed or price unknown
}

var opUsageRows = []Policy{
	{Op: OpAIUsage, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: slow, MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opUsageRows...); return true }()

func init() {
	registerResult(OpAIUsage, AIUsage{})
}
