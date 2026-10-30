package webproto

import (
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"
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
	// Gateway: the gateway answers it itself (it holds the passkeys);
	// it never reaches the hub, which refuses it.
	Gateway bool
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
	OpGitStageHunk     = "git.stage_hunk"
	OpGitUnstageHunk   = "git.unstage_hunk"
	OpGitCommit        = "git.commit"
	OpGitDiscard       = "git.discard"
	OpSearchRun        = "search.run"
	OpSearchOpen       = "search.open"
	OpSearchStop       = "search.stop"
	OpTasksRun         = "tasks.run"
	OpTasksTerminate   = "tasks.terminate"
	OpTasksTail        = "tasks.tail"
	OpRunsStop         = "runs.stop"
	OpRunsPause        = "runs.pause"
	OpRunsResume       = "runs.resume"
	OpWebOpen          = "web.open"
	OpWebStop          = "web.stop"
	OpWebPairAsk       = "web.pair_ask"
	OpWebGrantAsk      = "web.grant_ask"
	OpWebTotpCheck     = "web.totp_check"
	OpControlChallenge = "control.challenge"
	OpControlAsk       = "control.ask"
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
	{Op: OpTasksTail, New: newOf[LabelArgs](), Role: RoleViewer, Rate: perSec(5), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV1},
	{Op: OpSearchStop, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(20), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV1},
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
	{Op: OpGitStageHunk, New: newOf[GitHunkArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpGitUnstageHunk, New: newOf[GitHunkArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 8 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpGitCommit, New: newOf[GitCommitArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 40 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpGitDiscard, New: newOf[PathsArgs](), Role: RoleController, Rate: Rate{N: 1, Per: 5 * time.Second}, MaxBytes: 16 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV1},

	// Tasks and AI runs.
	{Op: OpTasksRun, New: newOf[LabelArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpTasksTerminate, New: newOf[LabelArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpRunsStop, New: newOf[KeyArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpRunsPause, New: newOf[KeyArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpRunsResume, New: newOf[KeyArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},

	// Asking for control (v2, WEB-34): a viewer proves it holds its
	// passkey (a fresh assertion) and the gateway asks the laptop.
	{Op: OpControlChallenge, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: Rate{N: 1, Per: 5 * time.Second}, MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2, Gateway: true},
	{Op: OpControlAsk, New: newOf[ControlAskArgs](), Role: RoleViewer, Rate: Rate{N: 1, Per: 5 * time.Second}, MaxBytes: 16 * kib, Audit: AuditYes, Phase: PhaseV2, Gateway: true},

	// Control connection only (same user, never a browser).
	{Op: OpWebOpen, New: newOf[WebOpenArgs](), Role: RoleCtl, Rate: perSec(1), MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV1},
	{Op: OpWebStop, New: newOf[EmptyArgs](), Role: RoleCtl, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV1},
	// The gateway's questions to the laptop (v2): pair a device (QR + SAS
	// code), give a viewer control, check a TOTP code against the secret
	// store. The answer is the owner's or the UI's, never the browser's.
	{Op: OpWebPairAsk, New: newOf[PairAskArgs](), Role: RoleCtl, Rate: perSec(1), MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWebGrantAsk, New: newOf[GrantAskArgs](), Role: RoleCtl, Rate: perSec(1), MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWebTotpCheck, New: newOf[TotpCheckArgs](), Role: RoleCtl, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
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
	if badText(raw) {
		return nil, invalid("text that is not UTF-8")
	}
	a := p.New()
	if err := unmarshalStrict(raw, a); err != nil {
		return nil, err
	}
	if v, ok := a.(validator); ok {
		if err := v.validate(len(raw)); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// Allowed reports whether a connection with role may call op. A
// controller may do what a viewer may; control ops are for the control
// connection only.
func Allowed(role Role, op string) bool {
	p, ok := Lookup(op)
	if !ok {
		return false
	}
	switch role {
	case RoleViewer:
		return p.Role == RoleViewer
	case RoleController:
		return p.Role == RoleViewer || p.Role == RoleController
	case RoleCtl:
		return p.Role == RoleCtl
	}
	return false
}

// CapsFor lists the browser ops a role may call, in table order; it is
// what meta.caps carries.
func CapsFor(role Role) []string {
	if role != RoleViewer && role != RoleController {
		return nil
	}
	var out []string
	for _, p := range opTable {
		if Allowed(role, p.Op) {
			out = append(out, p.Op)
		}
	}
	return out
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

// badText reports raw JSON with invalid UTF-8 or a lone surrogate escape
// ("\ud800"): the decoder would turn both into U+FFFD silently, and an
// edit must never change text the user did not type.
func badText(raw []byte) bool {
	if !utf8.Valid(raw) {
		return true
	}
	for i := 0; i+5 < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		if raw[i+1] != 'u' {
			i++ // an escaped char: skip it (it may be a backslash)
			continue
		}
		r, ok := hex4(raw[i+2 : i+6])
		switch {
		case !ok:
		case r >= 0xd800 && r < 0xdc00: // high: a low one must follow
			if i+12 > len(raw) {
				return true
			}
			if raw[i+6] != '\\' || raw[i+7] != 'u' {
				return true
			}
			lo, ok := hex4(raw[i+8 : i+12])
			if !ok || lo < 0xdc00 || lo >= 0xe000 {
				return true
			}
			i += 11
			continue
		case r >= 0xdc00 && r < 0xe000:
			return true
		}
		i += 5
	}
	return false
}

func hex4(b []byte) (rune, bool) {
	var r rune
	for _, c := range b {
		switch {
		case c >= '0' && c <= '9':
			r = r<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return r, true
}
