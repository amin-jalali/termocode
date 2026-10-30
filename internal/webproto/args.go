package webproto

// Args types of the requests. Paths are workspace-relative with forward
// slashes; the hub checks them with the workspace resolver. Buf is an nvim
// buffer number. No args type carries Lua, an RPC name, a command line or
// a key sequence for nvim (ADR 0020).

// EmptyArgs is the args of an op that takes none.
type EmptyArgs struct{}

// ChatOpenArgs pins the browser to one chat.
type ChatOpenArgs struct {
	ID string `json:"id"`
}

// ChatGetMessageArgs fetches the rest of a cut message.
type ChatGetMessageArgs struct {
	I int `json:"i"`
}

// ChatPinArgs turns "follow the laptop's chat" off (On) or back on.
type ChatPinArgs struct {
	On bool `json:"on"`
}

// ChatSendArgs sends a chat message.
type ChatSendArgs struct {
	Text   string `json:"text"`
	ChatID string `json:"chat_id,omitempty"`
}

// CardsAnswerArgs answers a card with one of its choices.
type CardsAnswerArgs struct {
	ID     string `json:"id"`
	Choice string `json:"choice"`
}

// PathArgs names one file or folder.
type PathArgs struct {
	Path string `json:"path"`
}

// PathsArgs names files for a git op.
type PathsArgs struct {
	Paths []string `json:"paths"`
}

// BufArgs names one nvim buffer.
type BufArgs struct {
	Buf int `json:"buf"`
}

// Pos is a zero-based position in a buffer (col in bytes, as nvim counts).
type Pos struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

// SetTextArgs replaces a range of a buffer if its changedtick is still Tick.
type SetTextArgs struct {
	Buf   int      `json:"buf"`
	Tick  int      `json:"tick"`
	Start Pos      `json:"start"`
	End   Pos      `json:"end"`
	Lines []string `json:"lines"`
}

// TermHistoryArgs pages older terminal rows (Before is a row index).
type TermHistoryArgs struct {
	Buf    int `json:"buf"`
	Before int `json:"before"`
}

// TermSendArgs types text into a terminal.
type TermSendArgs struct {
	Buf  int    `json:"buf"`
	Text string `json:"text"`
}

// GitDiffArgs asks for the diff of one file.
type GitDiffArgs struct {
	Path   string `json:"path"`
	Staged bool   `json:"staged,omitempty"`
	From   int    `json:"from,omitempty"` // the first row of the page (GitDiff.Next)
}

// GitShowArgs asks for one commit.
type GitShowArgs struct {
	Hash string `json:"hash"`
	From int    `json:"from,omitempty"`
}

// GitHunkArgs names one hunk of a file's diff by its exact header line
// (a "hunk" row's text): if the diff changed and has no such hunk now,
// nothing happens.
type GitHunkArgs struct {
	Path string `json:"path"`
	Hunk string `json:"hunk"`
}

// GitCommitArgs commits what is staged. There is no amend from the web.
type GitCommitArgs struct {
	Message string `json:"message"`
}

// SearchRunArgs starts a search in the workspace.
type SearchRunArgs struct {
	Query   string `json:"query"`
	Regex   bool   `json:"regex,omitempty"`
	Case    bool   `json:"case,omitempty"`
	Word    bool   `json:"word,omitempty"`
	Include string `json:"include,omitempty"`
	Exclude string `json:"exclude,omitempty"`
	Max     int    `json:"max,omitempty"`
}

// SearchOpenArgs opens a search hit in the file view.
type SearchOpenArgs struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

// LabelArgs names a task.
type LabelArgs struct {
	Label string `json:"label"`
}

// KeyArgs names an AI run.
type KeyArgs struct {
	Key string `json:"key"`
}

// WebOpenArgs starts the web side from the command line; Yes skips the
// start card (same user only, on the control connection). SSHPort (0:
// off) is `termocode ssh --web` (WEB-29): the gateway serves on a unix
// socket for the user's ssh forward of 127.0.0.1:SSHPort, and the answer
// carries the one-time secret for the far side to hand over. It always
// comes with Yes: the user typed the command themselves.
type WebOpenArgs struct {
	Yes     bool `json:"yes,omitempty"`
	SSHPort int  `json:"ssh_port,omitempty"`
}
