package webproto

// Quick open (the desktop workbench's Ctrl/Cmd+P): the workspace's file
// paths, as the laptop's "Go to file" lists them, so the page can find a
// file by a few letters of its name.

// FilesListRes is the result of files.list: workspace-relative paths
// (slash-separated), at most MaxQuickFiles; Truncated says there are more.
type FilesListRes struct {
	Paths     []string `json:"paths"`
	Truncated bool     `json:"truncated,omitempty"`
}

// MaxQuickFiles caps one files.list answer.
const MaxQuickFiles = 20000

func init() {
	registerResult(OpFilesList, FilesListRes{})
}
