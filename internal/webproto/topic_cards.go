package webproto

// The cards' data (web design 2.2): what waits for the user's answer.
// A card has one id on the laptop and on every browser; the first answer
// wins and everyone else sees "answered". The web answers only with the
// choices a card lists without needs_laptop: never "Always allow", never
// a trust choice (trust cards are read-only), never an edit.

func init() {
	registerTopic("cards", CardsSnap{}, CardsDelta{})
	registerEvent(EventAnswered, Answered{})
}

// Card kinds.
const (
	CardTool   = "tool"   // the chat's AI wants to run a tool
	CardMCP    = "mcp"    // an outside agent's call over MCP
	CardPlan   = "plan"   // a run waits for its plan to be approved
	CardReview = "review" // a proposed change to one file
	CardTrust  = "trust"  // a project profile, rules, skills or MCP server asks for trust (laptop only)
)

// Choice ids the web may send.
const (
	ChoiceDeny    = "deny"
	ChoiceAllow   = "allow" // "Allow once"
	ChoiceApprove = "approve"
	ChoiceAccept  = "accept"
	ChoiceReject  = "reject"
)

// MaxCardDiff caps the text of a review card's diff rows; more is cut and
// the page offers "Review on laptop".
const MaxCardDiff = 256 << 10

// CardsSnap is the cards topic: the waiting cards, oldest first.
type CardsSnap struct {
	Cards []Card `json:"cards"`
}

// Card is one waiting question. The first choice is the safe one; the page
// draws it first and focused.
type Card struct {
	ID       string       `json:"id"`
	Kind     string       `json:"kind" enum:"tool|mcp|plan|review|trust"`
	Title    string       `json:"title"`
	Body     string       `json:"body,omitempty"` // plain text
	Choices  []CardChoice `json:"choices"`
	Since    int64        `json:"since"`               // Unix ms it appeared
	ReadOnly bool         `json:"read_only,omitempty"` // answer it on the laptop
	File     string       `json:"file,omitempty"`      // review: the file (workspace-relative)
	Diff     *CardDiff    `json:"diff,omitempty"`      // review
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
}

// DiffRow is one row of a diff.
type DiffRow struct {
	K    string `json:"k" enum:"ctx|add|del"`
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
	ID string `json:"id"`
	By string `json:"by,omitempty"` // "laptop", a device name, or "" (expired)
}
