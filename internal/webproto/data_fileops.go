package webproto

// Results of the file ops (WEB-41).

func init() {
	registerResult(OpTreeNewFile, TreeChanged{})
	registerResult(OpTreeNewDir, TreeChanged{})
	registerResult(OpTreeRename, TreeChanged{})
	registerResult(OpTreeDelete, AskRef{})
}

// TreeChanged answers a file op that ran: the path it made or renamed to.
type TreeChanged struct {
	Path string `json:"path"`
}
