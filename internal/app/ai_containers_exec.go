package app

import (
	"context"

	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// aiCtrTerminal opens a container command in a terminal tab of the IDE
// (a seam for tests: they have no editor to run the tab in).
var aiCtrTerminal = func(m *Model, c services.Command, key string) error { return m.servicesRunCommand(c, key) }

// aiCtrFiles is the file system of container id on h (a seam for tests).
var aiCtrFiles = func(ctx context.Context, h aiContainersHost, id string) (workspace.FS, error) {
	e := h.e
	host, err := containerhost.New(ctx, containerhost.Config{Binary: e.Binary, Kind: e.Kind, Context: e.Context, Container: id, Root: "/", X: e.X})
	if err != nil {
		return nil, err
	}
	return host.FS(), nil
}
