package webproto

// The AI chat's data (web design 2.2, 3.6): the chat list, the open chat
// and its messages. Markdown travels as an AST of a few allowed node
// kinds, never as HTML: the page builds elements from the kinds and text
// nodes from the strings.

func init() {
	registerTopic("chats", ChatsSnap{}, ChatsDelta{})
	registerTopic("chat", ChatSnap{}, ChatDelta{})
	registerResult(OpChatOpen, ChatSnap{})
	registerResult(OpChatGetMessage, ChatMsg{})
	registerResult(OpChatNew, ChatRef{})
	registerResult(OpChatSend, ChatRef{})
}

// Chat limits.
const (
	MaxChats       = 200      // chats in the chats topic, newest first
	MaxChatMsgText = 64 << 10 // bytes of a message's text in a snap or delta; more is cut
	MaxChatMsgFull = 512 << 10
)

// ChatsSnap is the chats topic: the stored chats, newest first.
type ChatsSnap struct {
	Chats []ChatItem `json:"chats"`
}

// ChatItem is one chat in the list.
type ChatItem struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Updated int64  `json:"updated"` // Unix ms
	Msgs    int    `json:"msgs"`
	Open    bool   `json:"open,omitempty"` // the laptop shows this chat
	Live    bool   `json:"live,omitempty"` // the AI is answering in it now
}

// Chats delta ops.
const (
	ChatsUpsert = "upsert" // Chat is new or replaces the one with its id; the list stays sorted by updated, newest first
	ChatsRemove = "remove" // the chat with ID is gone
)

// ChatsDelta is one change of the chat list.
type ChatsDelta struct {
	Op   string    `json:"op" enum:"upsert|remove"`
	Chat *ChatItem `json:"chat,omitempty"`
	ID   string    `json:"id,omitempty"`
}

// ChatSnap is the chat topic: the chat open on the laptop (the page
// follows it; chat.open shows another one read-only). Also the result of
// chat.open.
type ChatSnap struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Profile string `json:"profile,omitempty"` // the active agent profile, "" for the default
	Model   string `json:"model,omitempty"`
	Busy    bool   `json:"busy"` // the AI is answering
	// Queue is what the phone lined up for after the turn (CMP-46).
	Queue    []ChatQueued `json:"queue,omitempty"`
	Messages []ChatMsg    `json:"messages"`
	// CtxPct is how full the model's context window was on the chat's
	// newest turn, in percent; CtxTokens and CtxWindow are the counts
	// behind it. All 0 while unknown (CMP-7).
	CtxPct    int `json:"ctx_pct,omitempty"`
	CtxTokens int `json:"ctx_tokens,omitempty"`
	CtxWindow int `json:"ctx_window,omitempty"`
}

// ChatMsg is one message, trimmed for the page.
type ChatMsg struct {
	I         int          `json:"i"` // its index in Messages
	Role      string       `json:"role" enum:"user|assistant|tool|error"`
	Text      string       `json:"text,omitempty"`      // plain text: tool and error lines, and an answer while it streams
	MD        *MdNode      `json:"md,omitempty"`        // user and finished assistant messages: the markdown as an AST
	Streaming bool         `json:"streaming,omitempty"` // the answer is still coming (text grows by append deltas)
	Truncated bool         `json:"truncated,omitempty"` // cut at MaxChatMsgText; chat.get_message {i} returns more
	Model     string       `json:"model,omitempty"`     // the model of an answer, when not the chat's own
	Images    []string     `json:"images,omitempty"`    // attached image file names
	Attached  []AttachChip `json:"attached,omitempty"`  // what the user sent back with it (MOB-5), ≤ MaxAttachedChips
	Rules     string       `json:"rules,omitempty"`     // the project rules the turn used ("AGENTS.md")
	Run       *ChatRun     `json:"run,omitempty"`
	Steps     []ChatStep   `json:"steps,omitempty"`
	Blocks    []Block      `json:"blocks,omitempty"` // typed visual blocks after the text, ≤ MaxMsgBlocks
	Meta      *ChatMeta    `json:"meta,omitempty"`   // a one-step profile's reply: the line under it
	Wait      *ChatWait    `json:"wait,omitempty"`   // the newest message of a running turn: what its tool waits for
}

// ChatWait is what a tool of the running turn waits for from the user
// (an AI db_* tool's question: the trust of an SSH host, an SSH password
// or key passphrase, the database's password). The page shows "Waiting
// for you: <Text>" on the message's tool line, with a button that opens
// the question (the ask ev with the same Conn and Need).
type ChatWait struct {
	Text string `json:"text"`           // "Database password for BaaytonaDB"
	Conn string `json:"conn,omitempty"` // the connection the question is for
	Need string `json:"need,omitempty"` // its DBNeed.Need
}

// ChatMeta is what a one-step profile's reply took: the line under the
// answer ("gpt-6-sol · 54s") and its tooltip (tokens, step, CLI).
type ChatMeta struct {
	ElapsedMs int64  `json:"elapsed_ms,omitempty"` // once it ended
	StartedMs int64  `json:"started_ms,omitempty"` // Unix ms; the page counts from it while it runs
	In        int    `json:"in,omitempty"`         // prompt tokens (cache included)
	Out       int    `json:"out,omitempty"`        // generated tokens
	Step      string `json:"step,omitempty"`       // the step's title
	CLI       string `json:"cli,omitempty"`        // the CLI that ran it ("Codex"), "" for an API model
}

// ChatRun is the pipeline run a message shows.
type ChatRun struct {
	ID        string  `json:"id"`
	Profile   string  `json:"profile,omitempty"`
	State     string  `json:"state" enum:"running|done|failed|cancelled|interrupted|paused"`
	ElapsedMs int64   `json:"elapsed_ms,omitempty"`
	CostUSD   float64 `json:"cost_usd,omitempty"`
	PlanWait  bool    `json:"plan_wait,omitempty"`  // waits for a plan approval (a card)
	StartedMs int64   `json:"started_ms,omitempty"` // Unix ms; the page counts from it while it runs
}

// ChatStep is one step of a run.
type ChatStep struct {
	ID     string   `json:"id"`
	Title  string   `json:"title,omitempty"`
	State  string   `json:"state,omitempty"` // pending, running, done, failed, skipped …
	Model  string   `json:"model,omitempty"`
	Tools  []string `json:"tools,omitempty"` // tool lines ("⚙ Read a.go → 10 lines")
	Text   string   `json:"text,omitempty"`  // plain, while the step runs
	MD     *MdNode  `json:"md,omitempty"`    // the answer as an AST once the step ended
	Error  string   `json:"error,omitempty"`
	Blocks []Block  `json:"blocks,omitempty"` // typed visual blocks of the step, ≤ MaxMsgBlocks
	// ElapsedMs is how long the step took (once it ended).
	ElapsedMs int64 `json:"elapsed_ms,omitempty"`
}

// Chat delta ops.
const (
	ChatAppend   = "append"   // Text goes at the end of message I's text (a streaming answer)
	ChatMessage  = "message"  // Msg is new (Msg.I == len(messages)) or replaces message Msg.I
	ChatTruncate = "truncate" // only the first N messages stay
	ChatState    = "state"    // Busy, Title, the queue and / or the context use changed
	ChatSwitched = "switched" // the laptop opened another chat: Chat is all of it
)

// ChatDelta is one change of the open chat.
type ChatDelta struct {
	Op    string    `json:"op" enum:"append|message|truncate|state|switched"`
	I     *int      `json:"i,omitempty"`     // append
	Text  string    `json:"text,omitempty"`  // append
	Msg   *ChatMsg  `json:"msg,omitempty"`   // message
	N     *int      `json:"n,omitempty"`     // truncate
	Busy  *bool     `json:"busy,omitempty"`  // state
	Title string    `json:"title,omitempty"` // state
	Chat  *ChatSnap `json:"chat,omitempty"`  // switched
	// Queue is the whole new queue (state; CMP-46).
	Queue *ChatQueueList `json:"queue,omitempty"`
	// CtxPct, CtxTokens and CtxWindow are the chat's new context use
	// (state; 0 = unknown again).
	CtxPct    *int `json:"ctx_pct,omitempty"`
	CtxTokens *int `json:"ctx_tokens,omitempty"`
	CtxWindow *int `json:"ctx_window,omitempty"`
}

// ChatRef is the result of chat.new and chat.send: the chat it went to.
type ChatRef struct {
	ID string `json:"id"`
}

// Markdown node kinds: the only ones a page ever builds.
const (
	MdDoc        = "doc"
	MdParagraph  = "p"
	MdHeading    = "h"
	MdEm         = "em"
	MdStrong     = "strong"
	MdCode       = "code"
	MdCodeBlock  = "codeblock"
	MdList       = "ul"
	MdOrdered    = "ol"
	MdItem       = "li"
	MdQuote      = "blockquote"
	MdLink       = "link"
	MdRule       = "hr"
	MdBreak      = "br"
	MdTable      = "table"
	MdHeadCell   = "th"
	MdCell       = "td"
	MdText       = "text"
	MdMaxDepth   = 32    // deeper nesting is flattened to text
	MdMaxNodes   = 20000 // a bigger AST ends in one text node with the rest
	MdMaxLangLen = 32
)

// MdNode (a markdown AST node) is defined in internal/blocks and
// aliased in blocks.go, so a text block carries the same AST.
