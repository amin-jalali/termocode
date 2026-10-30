package webproto

import (
	"encoding/json"
	"errors"
	"time"
)

// RoleCtl marks an op that only the gateway's control connection may send.
// It is never the role of a browser.
const RoleCtl Role = "ctl"

// Audit says what the audit log keeps of an op.
type Audit string

// Audit kinds.
const (
	AuditNo      Audit = "no"      // nothing
	AuditYes     Audit = "yes"     // one line per call, with a summary from the hub
	AuditCounted Audit = "counted" // a count per minute, never the content
)

// Rate is "N calls per Per" for one connection. The gateway enforces it;
// the hub checks it again with 2× headroom.
type Rate struct {
	N   int
	Per time.Duration
}

// Policy is one row of the op table: the single source for the gateway,
// the hub, the generated TypeScript types, the docs and the tests.
type Policy struct {
	Op          string
	New         func() any // a new, empty args value (pointer to struct)
	Role        Role       // the least role that may call it
	Rate        Rate
	MaxBytes    int  // cap on the raw args JSON
	BytesPerSec int  // extra byte rate cap (0 = none)
	Live        int  // calls of this op running at once per connection (0 = no extra cap)
	LaptopCard  bool // waits for a Risky card on the laptop
	Audit       Audit
	Phase       Phase
}

// Op names.
const (
	OpChatOpen         = "chat.open"
	OpChatGetMessage   = "chat.get_message"
	OpChatPin          = "chat.pin"
	OpChatSend         = "chat.send"
	OpChatStop         = "chat.stop"
	OpChatNew          = "chat.new"
	OpCardsAnswer      = "cards.answer"
	OpTreeList         = "tree.list"
	OpTreeUnlist       = "tree.unlist"
	OpTreeRevealLaptop = "tree.reveal_on_laptop"
	OpBufOpen          = "buf.open"
	OpBufHead          = "buf.head"
	OpBufSetText       = "buf.set_text"
	OpBufSave          = "buf.save"
	OpBufUndo          = "buf.undo"
	OpBufClose         = "buf.close"
	OpTermHistory      = "term.history"
	OpTermSend         = "term.send"
	OpTermInterrupt    = "term.interrupt"
	OpTermNew          = "term.new"
	OpTermShowOnLaptop = "term.show_on_laptop"
	OpTermClose        = "term.close"
	OpGitDiff          = "git.diff"
	OpGitShow          = "git.show"
	OpGitStage         = "git.stage"
	OpGitUnstage       = "git.unstage"
	OpGitCommit        = "git.commit"
	OpGitDiscard       = "git.discard"
	OpSearchRun        = "search.run"
	OpSearchOpen       = "search.open"
	OpTasksRun         = "tasks.run"
	OpTasksTerminate   = "tasks.terminate"
	OpRunsStop         = "runs.stop"
	OpRunsPause        = "runs.pause"
	OpRunsResume       = "runs.resume"
	OpWebOpen          = "web.open"
	OpWebStop          = "web.stop"
)

const (
	kib = 1 << 10
	mib = 1 << 20
)

func perSec(n int) Rate { return Rate{N: n, Per: time.Second} }

func newOf[T any]() func() any { return func() any { return new(T) } }

// The op table (v1). Reads are open to viewers; everything that types,
// edits or answers needs a controller. Laptop cards only for what cannot
// be undone and is easy to tap by mistake.
var opTable = []Policy{
	// Reads.
	{Op: OpChatOpen, New: newOf[ChatOpenArgs](), Role: RoleViewer, Rate: perSec(20), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV1},
	{Op: OpChatGetMessage, New: newOf[ChatGetMessageArgs](), Role: RoleViewer, Rate: perSec(20), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV1},
	{Op: OpChatPin, New: newOf[ChatPinArgs](), Role: RoleViewer, Rate: perSec(20), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV1},
	{Op: OpTreeList, New: newOf[PathArgs](), Role: RoleViewer, Rate: perSec(20), MaxBytes: 8 * kib, Audit: AuditNo, Phase: PhaseV1},
	{Op: OpTreeUnlist, New: newOf[PathArgs](), Role: RoleViewer, Rate: perSec(20), MaxBytes: 8 * kib, Audit: AuditNo, Phase: PhaseV1},
	{Op: OpBufOpen, New: newOf[PathArgs](), Role: RoleViewer, Rate: perSec(20), MaxBytes: 8 * kib, Audit: AuditNo, Phase: PhaseV1},
	{Op: OpBufHead, New: newOf[PathArgs](), Role: RoleViewer, Rate: perSec(20), MaxBytes: 8 * kib, Audit: AuditNo, Phase: PhaseV1},
	{Op: OpTermHistory, New: newOf[TermHistoryArgs](), Role: RoleViewer, Rate: perSec(20), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV1},
	{Op: OpGitDiff, New: newOf[GitDiffArgs](), Role: RoleViewer, Rate: perSec(20), MaxBytes: 8 * kib, Audit: AuditNo, Phase: PhaseV1},
	{Op: OpGitShow, New: newOf[GitShowArgs](), Role: RoleViewer, Rate: perSec(20), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV1},
	{Op: OpSearchRun, New: newOf[SearchRunArgs](), Role: RoleViewer, Rate: perSec(20), MaxBytes: 8 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpSearchOpen, New: newOf[SearchOpenArgs](), Role: RoleViewer, Rate: perSec(20), MaxBytes: 8 * kib, Audit: AuditNo, Phase: PhaseV1},

	// Chat and cards.
	{Op: OpChatSend, New: newOf[ChatSendArgs](), Role: RoleController, Rate: Rate{N: 1, Per: 2 * time.Second}, MaxBytes: 72 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpChatStop, New: newOf[EmptyArgs](), Role: RoleController, Rate: Rate{N: 1, Per: 2 * time.Second}, MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpChatNew, New: newOf[EmptyArgs](), Role: RoleController, Rate: Rate{N: 1, Per: 2 * time.Second}, MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpCardsAnswer, New: newOf[CardsAnswerArgs](), Role: RoleController, Rate: perSec(5), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},

	// Files.
	{Op: OpTreeRevealLaptop, New: newOf[PathArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 8 * kib, Audit: AuditNo, Phase: PhaseV1},
	{Op: OpBufSetText, New: newOf[SetTextArgs](), Role: RoleController, Rate: perSec(20), MaxBytes: 1 * mib, Audit: AuditCounted, Phase: PhaseV1},
	{Op: OpBufSave, New: newOf[BufArgs](), Role: RoleController, Rate: perSec(20), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpBufUndo, New: newOf[BufArgs](), Role: RoleController, Rate: perSec(20), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV1},
	{Op: OpBufClose, New: newOf[BufArgs](), Role: RoleController, Rate: perSec(20), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV1},

	// Terminals.
	{Op: OpTermSend, New: newOf[TermSendArgs](), Role: RoleController, Rate: perSec(100), MaxBytes: 136 * kib, BytesPerSec: 1 * mib, Audit: AuditCounted, Phase: PhaseV1},
	{Op: OpTermInterrupt, New: newOf[BufArgs](), Role: RoleController, Rate: perSec(100), MaxBytes: 1 * kib, Audit: AuditCounted, Phase: PhaseV1},
	{Op: OpTermNew, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(100), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpTermShowOnLaptop, New: newOf[BufArgs](), Role: RoleController, Rate: perSec(100), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV1},
	{Op: OpTermClose, New: newOf[BufArgs](), Role: RoleController, Rate: Rate{N: 1, Per: 5 * time.Second}, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV1},

	// Git.
	{Op: OpGitStage, New: newOf[PathsArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 16 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpGitUnstage, New: newOf[PathsArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 16 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpGitCommit, New: newOf[GitCommitArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 40 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpGitDiscard, New: newOf[PathsArgs](), Role: RoleController, Rate: Rate{N: 1, Per: 5 * time.Second}, MaxBytes: 16 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV1},

	// Tasks and AI runs.
	{Op: OpTasksRun, New: newOf[LabelArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpTasksTerminate, New: newOf[LabelArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpRunsStop, New: newOf[KeyArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpRunsPause, New: newOf[KeyArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpRunsResume, New: newOf[KeyArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},

	// Control connection only (same user, never a browser).
	{Op: OpWebOpen, New: newOf[WebOpenArgs](), Role: RoleCtl, Rate: perSec(1), MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpWebStop, New: newOf[EmptyArgs](), Role: RoleCtl, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},
}

// ErrUnknownOp is returned for an op with no policy row.
var ErrUnknownOp = errors.New("webproto: unknown op")

// DecodeArgs decodes the args of a request strictly into the op's args
// type: the raw size is capped by the row, an unknown field, a duplicate
// or case-folded key and trailing data are refused. Missing args are {}.
func DecodeArgs(op string, raw json.RawMessage) (any, error) {
	p, ok := Lookup(op)
	if !ok {
		return nil, ErrUnknownOp
	}
	if len(raw) > p.MaxBytes {
		return nil, ErrTooBig
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := checkKeys(raw); err != nil {
		return nil, err
	}
	a := p.New()
	if err := unmarshalStrict(raw, a); err != nil {
		return nil, err
	}
	return a, nil
}

// Ops returns a copy of the op table, in table order.
func Ops() []Policy { return append([]Policy(nil), opTable...) }

// Lookup returns the policy row of an op.
func Lookup(op string) (Policy, bool) {
	for _, p := range opTable {
		if p.Op == op {
			return p, true
		}
	}
	return Policy{}, false
}
