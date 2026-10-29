package ai

// Plan approval (G-PLAN): ai_plan_approval says when an agent run shows
// its plan and waits for the user before a step that may write files or
// run commands.
const (
	PlanApprovalAsk    = "ask"    // default: ask before steps that write files or run commands
	PlanApprovalAuto   = "auto"   // never ask (the old way)
	PlanApprovalAlways = "always" // ask before every plan, read-only ones too
)

// PlanApprovalModes are the values of ai_plan_approval.
var PlanApprovalModes = []string{PlanApprovalAsk, PlanApprovalAuto, PlanApprovalAlways}

// PlanApprovalMode is the effective ai_plan_approval: an unknown or
// empty value is "ask" (the safe default).
func (s Settings) PlanApprovalMode() string {
	return CheckPlanApproval(s.PlanApproval)
}

// CheckPlanApproval returns v when it is a known mode, else "ask".
func CheckPlanApproval(v string) string {
	switch v {
	case PlanApprovalAuto, PlanApprovalAlways:
		return v
	}
	return PlanApprovalAsk
}
