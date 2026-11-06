package app

// Workflows (WFX, ADR 0021) own the kind numbers 3500–3549 of the
// confirm cards (confirmKindEnum), pickers (pickerKindEnum) and prompts
// (promptKindEnum). No other feature takes a number in this range:
//
//   - 3500–3539: the workflow views, the trust card, run and dry-run
//     cards, enable / disable, undo.
//   - 3540–3549: the write gate card (internal/workflow/ledger).
//
// TestConfirmKindsAreUnique still checks that no two cards share one.
const (
	wfxKindFirst     = 3500 // first number WFX owns
	wfxWriteGateKind = 3540 // first number of the write gate range
	wfxKindLast      = 3549 // last number WFX owns
)
