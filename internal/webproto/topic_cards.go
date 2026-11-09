package webproto

// The cards' data (web design 2.2): what waits for the user's answer.
// A card has one id on the laptop and on every browser; the first answer
// wins and everyone else sees "answered". The web answers only with the
// choices a card lists without needs_laptop: never "Always allow", never
// a trust choice (trust cards are read-only), never an edit.

import "github.com/amin-jalali/termocode/internal/blocks"

func init() {
	registerTopic("cards", CardsSnap{}, CardsDelta{})
	registerEvent(EventAnswered, Answered{})
	registerEvent(EventAsk, AskCard{})
}

// Card kinds.
const (
	CardTool     = "tool"     // the chat's AI wants to run a tool
	CardMCP      = "mcp"      // an outside agent's call over MCP
	CardPlan     = "plan"     // a run waits for its plan to be approved
	CardReview   = "review"   // a proposed change to one file
	CardTrust    = "trust"    // a project profile, rules, skills or MCP server asks for trust (laptop only)
	CardLaptop   = "laptop"   // a web request that cannot be undone waits for the laptop (the web may only cancel it)
	CardWorkflow = "workflow" // a workflow step wants to write to an outside system
)

// Choice ids the web may send.
const (
	ChoiceDeny    = "deny"
	ChoiceAllow   = "allow" // "Allow once"
	ChoiceApprove = "approve"
	ChoiceAccept  = "accept"
	ChoiceReject  = "reject"
	ChoiceCancel  = "cancel"  // a laptop card's safe answer
	ChoiceConfirm = "confirm" // a laptop card's action: the laptop only
	ChoiceSend    = "send"    // a card with a form: send the answers (CardsAnswerArgs.Form)
)

// A card's form (MOB-12): at most MaxCardForm answers of at most
// MaxCardFormValue bytes each.
const (
	MaxCardForm      = 20
	MaxCardFormValue = 1000
)

// MaxCardDiff caps the text of a review card's diff rows; more is cut and
// the page offers "Review on laptop".
const MaxCardDiff = 256 << 10

// MaxCardHunks caps the changes a review card lists for a choice by
// change; a card with more is answered for the whole file.
const MaxCardHunks = 128

// CardsSnap is the cards topic: the waiting cards, oldest first.
type CardsSnap struct {
	Cards []Card `json:"cards"`
}

// Card is one waiting question. The first choice is the safe one; the page
// draws it first and focused.
type Card struct {
	ID       string       `json:"id"`
	Kind     string       `json:"kind" enum:"tool|mcp|plan|review|trust|laptop|workflow"`
	Title    string       `json:"title"`
	Body     string       `json:"body,omitempty"` // plain text
	Choices  []CardChoice `json:"choices"`
	Since    int64        `json:"since"`               // Unix ms it appeared
	ReadOnly bool         `json:"read_only,omitempty"` // answer it on the laptop
	File     string       `json:"file,omitempty"`      // review: the file (workspace-relative)
	Diff     *CardDiff    `json:"diff,omitempty"`      // review
	// Form: an MCP server's question the phone may answer (MOB-12):
	// plain fields only, never a secret one; sent back with ChoiceSend.
	Form []blocks.BlockFormField `json:"form,omitempty"`
	// Destructive marks a tool card whose call cannot be undone (no
	// "Always allow" class, or a database write): the phone's approval
	// sheet offers only "Approve on laptop" for it (MOBUX-12).
	Destructive bool `json:"destructive,omitempty"`
}

// CardChoice is one answer of a card.
type CardChoice struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Safe        bool   `json:"safe,omitempty"`         // the safe answer (deny, reject)
	NeedsLaptop bool   `json:"needs_laptop,omitempty"` // shown, but only the laptop can pick it
}

// CardDiff is a review card's change, as rows.
type CardDiff struct {
	Rows      []DiffRow `json:"rows"`
	Added     int       `json:"added"`
	Removed   int       `json:"removed"`
	Truncated bool      `json:"truncated,omitempty"` // cut at MaxCardDiff
	// Hunks are the changes, as rows of Rows, when there are two or more
	// (and the rows are whole): the page may accept some of them.
	Hunks []CardHunk `json:"hunks,omitempty"`
}

// CardHunk is one change of a review card: Rows[First:End].
type CardHunk struct {
	First int `json:"first"`
	End   int `json:"end"`
}

// DiffRow is one row of a diff.
type DiffRow struct {
	K    string `json:"k" enum:"ctx|add|del|hunk|file"` // hunk and file rows come in git diffs only
	Text string `json:"text"`
	Old  int    `json:"old,omitempty"` // 1-based line in the old text
	New  int    `json:"new,omitempty"` // 1-based line in the new text
}

// Cards delta ops.
const (
	CardsUpsert = "upsert" // Card is new (goes last) or replaces the one with its id
	CardsRemove = "remove" // the card with ID is gone: answered (By) or expired
)

// CardsDelta is one change of the cards topic.
type CardsDelta struct {
	Op   string `json:"op" enum:"upsert|remove"`
	Card *Card  `json:"card,omitempty"`
	ID   string `json:"id,omitempty"`
	By   string `json:"by,omitempty"` // remove: "laptop" or the browser's device name; "" when it expired
}

// EventAnswered is the ev kind sent to every browser when a card got its
// answer (or went away): the page closes it and says who answered.
const EventAnswered = "answered"

// Answered is the data of an answered ev.
type Answered struct {
	ID     string `json:"id"`
	By     string `json:"by,omitempty"`     // "laptop", a device name, or "" (expired)
	Choice string `json:"choice,omitempty"` // a laptop card: "confirm" or "cancel"
}

// EventAsk is the ev kind sent only to the page that made a request
// which needs a confirm (an op whose row says LaptopCard), when the page
// approves its own actions (meta.approve_here): the card's exact text,
// the same the laptop card shows. The page answers it with cards.answer
// (confirm or cancel); only the asking connection may confirm.
const EventAsk = "ask"

// AskCard is the data of an ask ev.
type AskCard struct {
	ID     string `json:"id"`     // the card id the op answered with
	Title  string `json:"title"`  // "Discard changes?"
	Body   string `json:"body"`   // what runs, exactly (paths, the command argv, the SQL)
	Action string `json:"action"` // the confirm button's label ("Discard")
	// Conn and Need: a question an AI db_* tool waits on (an unknown SSH
	// host key, an SSH secret, the database's password), sent to every
	// controller page while the laptop asks it too. The page shows the
	// Databases view's own card and answers with db.trust_host or
	// db.secret (the value goes to the laptop only, never to the model),
	// or cancels with cards.answer. The first answer on any side wins.
	Conn string  `json:"conn,omitempty"`
	Need *DBNeed `json:"need,omitempty"`
}
