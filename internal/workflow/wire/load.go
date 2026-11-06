package wire

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
)

// ProjectDir is the workflow folder inside a repo.
const ProjectDir = ".termocode/workflows"

// Entry is one workflow file found on disk.
type Entry struct {
	Name    string
	Path    string
	Private bool // from the user's own folder (always trusted)
	Data    []byte
	Hash    string         // of Data (workflow.HashBytes)
	File    *workflow.File // nil: the file is broken (see Problems)
	Warns   []workflow.Problem
	// Problems are the errors that make the file unusable.
	Problems []workflow.Problem
	// Trusted: a private file, or a repo file whose hash the user trusted.
	Trusted bool
	Enabled bool // its schedule is on (state outside the repo)
}

// Broken reports whether the file cannot run.
func (e Entry) Broken() bool { return e.File == nil }

// Load reads every *.jsonc in the repo's ProjectDir and in private (""
// = none), sorted by name. On the same name the project file wins.
// root is the workflow state folder (ledger.Root) for trust and state.
func Load(workspace, private, root string) []Entry {
	byName := map[string]Entry{}
	if private != "" {
		for _, e := range loadDir(private, true, root) {
			byName[e.Name] = e
		}
	}
	if workspace != "" {
		for _, e := range loadDir(filepath.Join(workspace, filepath.FromSlash(ProjectDir)), false, root) {
			byName[e.Name] = e
		}
	}
	out := make([]Entry, 0, len(byName))
	for _, e := range byName {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Find returns the entry named name.
func Find(list []Entry, name string) (Entry, bool) {
	for _, e := range list {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// Trust stores that the user accepted e's bytes (the trust card).
func Trust(root string, e Entry) error {
	if e.Hash == "" {
		return errors.New("workflow: nothing to trust")
	}
	st, err := ledger.LoadState(root, e.Name, e.Hash)
	if err != nil {
		return err
	}
	st.TrustedHash = e.Hash
	return ledger.SaveState(root, e.Name, st)
}

func loadDir(dir string, private bool, root string) []Entry {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Entry
	for _, de := range ents {
		n := de.Name()
		if de.IsDir() || !strings.HasSuffix(n, ".jsonc") || strings.HasPrefix(n, ".") {
			continue
		}
		out = append(out, loadOne(filepath.Join(dir, n), private, root))
	}
	return out
}

func loadOne(path string, private bool, root string) Entry {
	e := Entry{Name: workflow.NameFromPath(path), Path: path, Private: private}
	data, err := readCapped(path, workflow.MaxFileBytes)
	if err != nil {
		e.Problems = []workflow.Problem{{Msg: err.Error()}}
		return e
	}
	e.Data, e.Hash = data, workflow.HashBytes(data)
	f, warns, err := workflow.Parse(path, data)
	e.Warns = warns
	if err != nil {
		var pe *workflow.ParseError
		if errors.As(err, &pe) {
			e.Problems = pe.Problems
		} else {
			e.Problems = []workflow.Problem{{Msg: err.Error()}}
		}
	} else {
		e.File = f
	}
	if root != "" {
		if st, err := ledger.LoadState(root, e.Name, e.Hash); err == nil {
			e.Enabled = st.Enabled
			e.Trusted = st.TrustedHash != "" && st.TrustedHash == e.Hash
		}
	}
	if private {
		e.Trusted = true
	}
	return e
}

func readCapped(path string, max int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > max {
		return nil, fmt.Errorf("the file is over %d KB", max>>10)
	}
	return b, nil
}
