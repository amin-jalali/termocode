package webproto

import (
	"strings"
	"time"
)

// The AI ops of the desktop editor (WEBP-3): ghost text and the next edit
// suggestion. The laptop does the work with its own provider, model,
// prompts and suggestion policy (the same code as its own editor); the
// page only says where the caret is and shows the answer. Both are
// controller ops (they spend the user's tokens and only help typing);
// the laptop's ai_inline / ai_next_edit settings decide whether they run,
// and the web cannot change them. A file that may hold secrets is never
// sent to the model.

// AI completion op names.
const (
	OpAIComplete = "ai.complete"
	OpAINextEdit = "ai.next_edit"
)

// OpAIAction is the editor's AI actions (Explain, Fix, Edit, Doc), as the
// laptop's editor has them: Explain asks in the laptop's open chat; the
// others answer with the lines to replace, which the page shows as a
// diff to accept or reject (nothing changes before the user accepts).
const OpAIAction = "ai.action"

// AI action kinds.
const (
	AIExplain = "explain"
	AIFix     = "fix"
	AIEdit    = "edit"
	AIDoc     = "doc"
)

// MaxAIInstruction caps an Edit instruction.
const MaxAIInstruction = 2 << 10

// AIActionArgs runs one AI action on buffer Buf as the page has it at
// changedtick Tick: the selected rows Start…End (zero-based, End not
// included; Start == End: no selection) and the caret's row Line.
// Instruction is Edit's (what to change).
type AIActionArgs struct {
	Buf         int    `json:"buf"`
	Tick        int    `json:"tick"`
	Kind        string `json:"kind" enum:"explain|fix|edit|doc"`
	Start       int    `json:"start"`
	End         int    `json:"end"`
	Line        int    `json:"line"`
	Instruction string `json:"instruction,omitempty"`
}

func (a *AIActionArgs) validate(int) error {
	if !validBuf(a.Buf) || a.Tick < 0 || a.Tick > MaxLspRow || a.Start < 0 || a.End < a.Start || a.End > MaxLspRow || a.Line < 0 || a.Line > MaxLspRow {
		return invalid("bad range")
	}
	switch a.Kind {
	case AIExplain, AIFix, AIDoc:
		if a.Instruction != "" {
			return invalid("an instruction is only for edit")
		}
	case AIEdit:
		if strings.TrimSpace(a.Instruction) == "" || len(a.Instruction) > MaxAIInstruction || hasControl(a.Instruction) {
			return invalid("bad instruction")
		}
	default:
		return invalid("bad kind")
	}
	return nil
}

// AIAction is the result of ai.action. Explain: Chat (the question went
// to the laptop's open chat, ChatID). The others: replace rows
// Start…End (zero-based, End not included) with Lines; None: the AI
// suggests no change. Title names the change ("AI fix").
type AIAction struct {
	Kind   string   `json:"kind" enum:"explain|fix|edit|doc"`
	Chat   bool     `json:"chat,omitempty"`
	ChatID string   `json:"chatId,omitempty"`
	None   bool     `json:"none,omitempty"`
	Title  string   `json:"title,omitempty"`
	Start  int      `json:"start"`
	End    int      `json:"end"`
	Lines  []string `json:"lines"`
}

// Limits of the AI completion answers.
const (
	MaxAIInsert    = 8 << 10 // bytes of one ghost text
	MaxAIImports   = 8       // import lines one ghost text adds
	MaxAIEditLines = 200     // lines of one next edit (old and new)
)

// AICompleteArgs asks for ghost text at a caret of buffer Buf as the page
// has it at changedtick Tick: Line zero-based, Col in UTF-16 units of
// the row. Manual: the user asked (Alt+\); the laptop's policy then does
// not skip the place.
type AICompleteArgs struct {
	Buf    int  `json:"buf"`
	Tick   int  `json:"tick"`
	Line   int  `json:"line"`
	Col    int  `json:"col"`
	Manual bool `json:"manual,omitempty"`
}

func (a *AICompleteArgs) validate(int) error {
	if !validBuf(a.Buf) || a.Tick < 0 || a.Line < 0 || a.Col < 0 || a.Line > MaxLspRow || a.Col > MaxLspRow || a.Tick > MaxLspRow {
		return invalid("bad position")
	}
	return nil
}

// AICompletion is the result of ai.complete: replace the row's text from
// UTF-16 column From up to the caret (To) with Text. From < To when the
// model rewrote what was typed ("print" → "fmt.Println()"). Imports are
// whole lines to insert before row ImportAt (zero-based). None: nothing
// to suggest here; Why says why in a few words when the user asked.
type AICompletion struct {
	None     bool     `json:"none,omitempty"`
	Why      string   `json:"why,omitempty"`
	Line     int      `json:"line"`
	From     int      `json:"from"`
	To       int      `json:"to"`
	Text     string   `json:"text"`
	ImportAt int      `json:"importAt,omitempty"`
	Imports  []string `json:"imports,omitempty"`
}

// AINextEditArgs says the user paused after an edit at Line (zero-based)
// of buffer Buf at changedtick Tick. Seed: the editor just opened; the
// laptop keeps the text to compare the next edit with and asks nothing.
type AINextEditArgs struct {
	Buf  int  `json:"buf"`
	Tick int  `json:"tick"`
	Line int  `json:"line"`
	Seed bool `json:"seed,omitempty"`
}

func (a *AINextEditArgs) validate(int) error {
	if !validBuf(a.Buf) || a.Tick < 0 || a.Line < 0 || a.Line > MaxLspRow || a.Tick > MaxLspRow {
		return invalid("bad position")
	}
	return nil
}

// AINextEdit is the result of ai.next_edit: replace rows Start…End
// (zero-based, End included) — whose text is Old — with Lines. None:
// no suggestion.
type AINextEdit struct {
	None  bool     `json:"none,omitempty"`
	Start int      `json:"start"`
	End   int      `json:"end"`
	Old   []string `json:"old"`
	Lines []string `json:"lines"`
}

var opAIRows = []Policy{
	{Op: OpAIComplete, New: newOf[AICompleteArgs](), Role: RoleController, Rate: perSec(5), MaxBytes: 1 * kib, Live: 2, Audit: AuditCounted, Phase: PhaseV2},
	{Op: OpAINextEdit, New: newOf[AINextEditArgs](), Role: RoleController, Rate: perSec(5), MaxBytes: 1 * kib, Live: 2, Audit: AuditCounted, Phase: PhaseV2},
	{Op: OpAIAction, New: newOf[AIActionArgs](), Role: RoleController, Rate: Rate{N: 1, Per: 2 * time.Second}, MaxBytes: 4 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opAIRows...); return true }()

func init() {
	registerResult(OpAIComplete, AICompletion{})
	registerResult(OpAINextEdit, AINextEdit{})
	registerResult(OpAIAction, AIAction{})
}
