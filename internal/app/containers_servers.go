package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
)

// The hosts tree of CONTEXTS (not built yet).

const (
	hostServerContainer hostKind = 101 // a container under a host of CONTEXTS
	hostAddServer       hostKind = 102 // the "+ Add Server…" row
)

// serverListContainers lists one host's containers (tests replace it).
var serverListContainers = func(ctx context.Context, e containers.Engine) ([]containers.Container, error) {
	return e.ListContainers(ctx)
}

// serverListMsg is one host's list.
type serverListMsg struct {
	context string
	gen     uint64
	rows    []containers.Container
	err     error
}

func (m *Model) onServerList(msg serverListMsg) tea.Cmd { return nil }
