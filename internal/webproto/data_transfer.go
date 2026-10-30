package webproto

// Results of the file transfer ops (WEB-49).

func init() {
	registerResult(OpFileDownload, FileChunk{})
	registerResult(OpFileUploadBegin, UploadStarted{})
	registerResult(OpFileUploadChunk, UploadProgress{})
	registerResult(OpFileUploadSave, TreeChanged{})
	registerResult(OpFileUploadReplace, AskRef{})
}

// FileChunk answers file.download: Data (base64) holds the bytes from
// From; Next is the From of the next chunk, 0 at the end.
type FileChunk struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	From  int64  `json:"from"`
	Data  string `json:"data"`
	Next  int64  `json:"next,omitempty"`
	Mtime int64  `json:"mtime"` // Unix ms; a change between chunks fails the download
}

// UploadStarted answers file.upload_begin.
type UploadStarted struct {
	ID     string `json:"id"`
	Chunk  int    `json:"chunk"`            // bytes per file.upload_chunk
	Exists bool   `json:"exists,omitempty"` // a file has this name: saving needs file.upload_replace
}

// UploadProgress answers file.upload_chunk.
type UploadProgress struct {
	Got int64 `json:"got"` // bytes the hub has
}
