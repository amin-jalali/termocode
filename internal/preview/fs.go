package preview

import (
	"context"
	"fmt"

	"github.com/amin-jalali/termocode/internal/workspace"
)

// FSFileMax is the largest file ReadFileFS reads for a preview: a
// workspace FS (a container) reads whole files, never a part.
const FSFileMax = 1 << 20

// ReadFileFS is ReadFile for a file reached through a workspace FS (a
// container workspace): a stat, then one listing or one read.
func ReadFileFS(ctx context.Context, fsys workspace.FS, path string) FileContent {
	if SecretPath(path) {
		return FileContent{Kind: "secret", Text: "hidden: may contain secrets · Enter to open"}
	}
	fi, err := fsys.Stat(ctx, path)
	if err != nil {
		return FileContent{Kind: "error", Text: err.Error()}
	}
	if fi.IsDir() {
		entries, err := fsys.ReadDir(ctx, path)
		if err != nil {
			return FileContent{Kind: "error", Text: err.Error()}
		}
		files, dirs := 0, 0
		for _, e := range entries {
			if e.IsDir() {
				dirs++
			} else {
				files++
			}
		}
		return FileContent{Kind: "folder", Text: fmt.Sprintf("%d files, %d folders", files, dirs)}
	}
	if !fi.Mode().IsRegular() {
		return FileContent{Kind: "binary", Text: "No preview for this file"}
	}
	if fi.Size() > FSFileMax {
		return FileContent{Kind: "large", Text: "Large file · " + fileSize(fi.Size()) + " · Enter to open"}
	}
	data, err := fsys.ReadFile(ctx, path)
	if err != nil {
		return FileContent{Kind: "error", Text: err.Error()}
	}
	return ClassifyFile(path, data, int64(len(data)))
}
