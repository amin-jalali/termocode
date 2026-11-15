package ai

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
)

// FileLedger is the set of files one chat has read or written. write_file
// on a file that exists and is not in it is refused: the model would
// replace code it never saw.
type FileLedger struct {
	mu   sync.Mutex
	seen map[string]bool
}

// NewFileLedger is an empty ledger (a new chat).
func NewFileLedger() *FileLedger { return &FileLedger{seen: map[string]bool{}} }

func (l *FileLedger) mark(abs string) {
	l.mu.Lock()
	l.seen[abs] = true
	l.mu.Unlock()
}

func (l *FileLedger) has(abs string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seen[abs]
}

// guardMarks are the tools whose success puts their path in the ledger.
var guardMarks = map[string]bool{"read_file": true, "write_file": true, "create_file": true, "edit_file": true}

// GuardWrites wraps the file tools of a chat turn with l: a successful
// read_file, write_file, create_file or edit_file marks its file, and
// write_file on an existing file that is not marked fails its Validate,
// so it is refused before any card. Apply it last, over the tools' final
// Run (review, IDE sync).
func GuardWrites(tools []Tool, w Workspace, l *FileLedger) []Tool {
	out := make([]Tool, len(tools))
	for i, t := range tools {
		if guardMarks[t.Name] {
			run := t.Run
			t.Run = func(ctx context.Context, a map[string]any) (string, error) {
				res, err := run(ctx, a)
				if err == nil {
					if abs, rerr := w.Resolve(ArgString(a, "path")); rerr == nil {
						l.mark(abs)
					}
				}
				return res, err
			}
		}
		if t.Name == "write_file" {
			validate := t.Validate
			t.Validate = func(a map[string]any) error {
				if validate != nil {
					if err := validate(a); err != nil {
						return err
					}
				}
				abs, err := w.Resolve(ArgString(a, "path"))
				if err != nil {
					return err
				}
				if l.has(abs) || !w.exists(abs) {
					return nil
				}
				return fmt.Errorf("%s already exists and you have not read it in this chat: read it first (read_file), or use edit_file to change part of it", w.rel(abs))
			}
		}
		out[i] = t
	}
	return out
}

// exists reports whether abs is there (on the workspace's host when it
// has one). An error other than "not there" counts as there: the guard
// stays on.
func (w Workspace) exists(abs string) bool {
	var err error
	if w.FS != nil {
		ctx, cancel := hostCtx(context.Background())
		defer cancel()
		_, err = w.FS.Stat(ctx, abs)
	} else {
		_, err = os.Lstat(abs)
	}
	return !errors.Is(err, fs.ErrNotExist)
}
