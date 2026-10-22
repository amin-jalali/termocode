package migrate

import (
	"fmt"
	"path/filepath"
)

// Read reads the editor files of a detected source. It writes nothing.
func Read(s Source) (Document, error) {
	switch s.ID {
	case "vscode", "vscodium", "cursor", "windsurf":
		return ReadVSCode(s.Dir)
	case "zed":
		return ReadZed(s.Dir)
	case "helix":
		return ReadHelix(s.Dir)
	case "jetbrains":
		return ReadJetBrains(s.Dir)
	case "sublime":
		return ReadSublime(s.Dir)
	case "vim":
		return ReadVim(filepath.Join(s.Dir, ".vimrc"))
	}
	return Document{}, fmt.Errorf("unknown editor %q", s.ID)
}
