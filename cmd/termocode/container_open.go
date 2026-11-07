package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/services"
)

// containerOpen is `termocode --container <name> [--context c] /folder`
// (plan RC, phase 3): open a folder inside a running container. The UI
// stays local; the container is reached only through the engine CLI.
type containerOpen struct {
	Name    string
	Context string
	Root    string
}

// The parts of the container open flow tests replace.
var (
	containerEngine services.Exec = services.SpawnExec{}
	setWorkspace                  = app.SetWorkspace
)

// flagValue reads "--flag v" or "--flag=v" at args[i]. ok is false when
// args[i] is not the flag; n is how many args it took.
func flagValue(args []string, i int, flag string) (v string, n int, ok bool) {
	a := args[i]
	if a == flag {
		if i+1 < len(args) {
			return args[i+1], 2, true
		}
		return "", 1, true
	}
	if v, found := strings.CutPrefix(a, flag+"="); found {
		return v, 1, true
	}
	return "", 0, false
}

// parseContainerOpen takes --container, --context and the folder out of
// args. With no --container it returns args unchanged and a zero value.
func parseContainerOpen(args []string) (containerOpen, []string, error) {
	var o containerOpen
	var rest, folders []string
	seen, ctxSeen := false, false
	for i := 0; i < len(args); {
		if v, n, ok := flagValue(args, i, "--container"); ok {
			if v == "" || strings.HasPrefix(v, "-") {
				return containerOpen{}, nil, errors.New("--container needs a container name")
			}
			o.Name, seen = v, true
			i += n
			continue
		}
		if v, n, ok := flagValue(args, i, "--context"); ok {
			if v == "" || strings.HasPrefix(v, "-") {
				return containerOpen{}, nil, errors.New("--context needs an engine context name")
			}
			o.Context, ctxSeen = v, true
			i += n
			continue
		}
		if !strings.HasPrefix(args[i], "-") {
			folders = append(folders, args[i])
		} else {
			rest = append(rest, args[i])
		}
		i++
	}
	if !seen {
		if ctxSeen {
			return containerOpen{}, nil, errors.New("--context only works with --container")
		}
		return containerOpen{}, args, nil
	}
	switch {
	case len(folders) == 0:
		return containerOpen{}, nil, errors.New("give the folder inside the container, like /app")
	case len(folders) > 1:
		return containerOpen{}, nil, errors.New("give only one folder inside the container")
	case !path.IsAbs(folders[0]):
		return containerOpen{}, nil, fmt.Errorf("the folder in the container must be absolute: %s", folders[0])
	}
	o.Root = path.Clean(folders[0])
	return o, rest, nil
}

// runContainer opens o and starts the UI on it. It runs without a
// persistent session: sessions serve local folders only for now.
func runContainer(o containerOpen) int {
	h, err := containerhost.New(context.Background(), containerhost.Config{
		Context:   o.Context,
		Container: o.Name,
		Root:      o.Root,
		X:         containerEngine,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "termocode: cannot open %s:%s: %v\n", o.Name, o.Root, err)
		return 1
	}
	setWorkspace(h.Workspace())
	startTUI()
	return 0
}
