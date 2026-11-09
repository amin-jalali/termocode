package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/containerhost"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// containers_exec: inside a container of any host, as the Containers
// view does it. shell (e), attach (a) and exec (Exec with Options…: a
// command, a user, a folder) open a terminal tab in the IDE; debug starts
// the toolbox container (Debug Selected with Toolbox…); files and read
// browse its files (Browse Files); upload puts a workspace file in;
// pair_sync starts Pair Sync with Workspace, whose own card lists every
// copy and delete for the user.
//
// It asks where the GUI asks: shell, attach, files, read and pair_sync
// run at once (the GUI opens them at once; Pair Sync asks on its own
// card), exec asks with the containers class (the GUI's prompts), debug
// and upload ask on a card every time, as the GUI's cards.

var aiExecOps = []string{"shell", "attach", "exec", "debug", "files", "read", "upload", "pair_sync"}

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

// aiContainersArgList is a list argument: a JSON array, or one string split at spaces.
func aiContainersArgList(a map[string]any, key string) ([]string, error) {
	switch v := a[key].(type) {
	case nil:
		return nil, nil
	case string:
		return strings.Fields(v), nil
	case []string:
		return v, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			s, ok := x.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be a list of strings", key)
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s must be a list of strings", key)
}

// aiExecReq is one checked containers_exec call.
type aiExecReq struct {
	op      string
	h       aiContainersHost
	c       containers.Container
	cmd     services.Command // shell, attach, exec, debug
	key     string           // the tab
	path    string           // files, read, upload (the container folder), pair_sync
	local   string           // upload: the workspace file
	replace bool             // upload: over a file of the same name
}

func aiExecParse(ctx context.Context, call aiIDECaller, a map[string]any) (aiExecReq, error) {
	r := aiExecReq{op: ai.ArgString(a, "op")}
	if !contains(aiExecOps, r.op) {
		return r, errors.New("give op: " + strings.Join(aiExecOps, ", "))
	}
	if err := aiValidContextArg(a); err != nil {
		return r, err
	}
	h, c, err := aiResolveContainer(ctx, call, a, strings.TrimSpace(ai.ArgString(a, "target")))
	r.h, r.c = h, c
	if err != nil {
		return r, err
	}
	name := containerShownName(c)
	if !strings.EqualFold(c.State, "running") {
		return r, fmt.Errorf("%s is %s: start it first (containers_control action start)", name, strings.ToLower(c.State))
	}
	switch r.op {
	case "shell", "attach", "exec":
		o := containers.ExecOptions{User: strings.TrimSpace(ai.ArgString(a, "user")), Workdir: strings.TrimSpace(ai.ArgString(a, "workdir"))}
		switch r.op {
		case "shell":
			o.Shell = strings.TrimSpace(ai.ArgString(a, "shell"))
		case "attach":
			r.cmd = h.e.Attach(c.ID)
		case "exec":
			if o.Command, err = aiContainersArgList(a, "command"); err != nil {
				return r, err
			}
			if len(o.Command) == 0 {
				return r, errors.New("exec: give command, like [\"ls\", \"-la\", \"/app\"] (op shell opens a shell)")
			}
		}
		if r.op != "attach" {
			if r.cmd, err = h.e.ExecCommand(c.ID, o); err != nil {
				return r, err
			}
		}
	case "debug":
		if r.cmd, err = h.e.DebugToolbox(c.ID, strings.TrimSpace(ai.ArgString(a, "image"))); err != nil {
			return r, err
		}
	case "files", "read", "upload", "pair_sync":
		r.path = strings.TrimSpace(ai.ArgString(a, "path"))
		if r.path == "" && r.op == "files" {
			r.path = "/"
		}
		if !path.IsAbs(r.path) || strings.ContainsAny(r.path, "\n\x00") {
			return r, errors.New("path: give an absolute path in the container, like /app")
		}
		r.path = path.Clean(r.path)
		if r.op == "pair_sync" {
			if _, err := pairSyncFolder(r.path); err != nil {
				return r, err
			}
			if h.picked != "" {
				return r, errors.New("pair_sync works on the session's host only (contexts_manage op use switches it)")
			}
		}
		if r.op == "upload" {
			rel := strings.TrimSpace(ai.ArgString(a, "file"))
			if r.local, err = aiDBInside(aiCtrRoot(h), rel); err != nil {
				return r, fmt.Errorf("file: %v", err)
			}
			r.replace = ai.ArgBool(a, "replace")
			if fi, err := os.Stat(r.local); err != nil || !fi.Mode().IsRegular() {
				return r, fmt.Errorf("file: %s is not a file in the workspace", rel)
			}
		}
	}
	if r.cmd.Argv != nil {
		r.cmd = h.termCommand(r.cmd)
		r.cmd.Label = r.op + ": " + name
		if h.picked != "" {
			r.cmd.Label += " @ " + h.picked
		}
		r.key = containerTabPrefix + r.op + ":" + h.tabKey(c.ID)
	}
	return r, nil
}

func aiExecDescribe(call aiIDECaller, a map[string]any) string {
	op, target := ai.ArgString(a, "op"), strings.TrimSpace(ai.ArgString(a, "target"))
	ctx, cancel := aiContainersDescribeCtx()
	defer cancel()
	r, err := aiExecParse(ctx, call, a)
	if err != nil {
		return aiTitle(strings.ReplaceAll(op+" ", "_", " ")) + "in " + target + " (refused: " + aiOneLineClip(err.Error(), 120) + ")"
	}
	name, where := containerShownName(r.c), r.h.contextLabel(ctx)
	switch r.op {
	case "debug":
		return fmt.Sprintf("Debug %s with a toolbox (%s) in a terminal tab · %s · a separate container shares its processes and network; %s itself is not changed",
			name, svcShellLine(r.cmd.Argv), where, name)
	case "upload":
		return fmt.Sprintf("Upload %s into %s at %s · %s", filepath.Base(r.local), aiContainerName(r.c), r.path, where)
	case "files", "read":
		return aiTitle(r.op) + " " + r.path + " in " + name
	case "pair_sync":
		return "Pair sync " + name + ":" + r.path + " with the workspace (the IDE's card lists every change)"
	}
	return fmt.Sprintf("%s in %s (%s) in a terminal tab · %s", aiTitle(r.op), name, svcShellLine(r.cmd.Argv), where)
}

func aiExecRun(ctx context.Context, call aiIDECaller, a map[string]any) (string, error) {
	r, err := aiExecParse(ctx, call, a)
	if err != nil {
		return "", err
	}
	name := containerShownName(r.c)
	switch r.op {
	case "files", "read", "upload":
		fctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		fsys, err := aiCtrFiles(fctx, r.h, r.c.ID)
		if err != nil {
			return "", fmt.Errorf("cannot open the files of %s: %s", name, errLine(aiContainersRedact(err.Error())))
		}
		return aiExecFiles(fctx, r, fsys)
	case "pair_sync":
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			if _, remote := m.remoteFS(); remote {
				return "", nil, errors.New("pair sync copies between a container and a folder on this machine; open a local folder first")
			}
			st := &m.services.pair
			if st.busy {
				return "", nil, errors.New("a pair sync is running; wait for it to finish")
			}
			st.c, st.name = r.c, name
			return "Pair sync of " + name + ":" + r.path + " with the workspace started: the IDE compares both sides, then its card lists " +
				"every copy and delete and the user answers it. Nothing is copied before that.", m.onPairSyncFolderPrompt(r.path), nil
		})
	}
	return call(ctx, func(m *Model) (string, tea.Cmd, error) {
		if r.op != "debug" && m.servicesFocusTab(r.key) {
			if _, running := m.svcTabRunning(r.key); running {
				return "focused the open terminal tab \"" + r.cmd.Label + "\" (no new tab)", nil, nil
			}
		}
		if err := aiCtrTerminal(m, r.cmd, r.key); err != nil {
			return "", nil, fmt.Errorf("cannot open the %s tab: %v", r.op, err)
		}
		return fmt.Sprintf("opened `%s` in the terminal tab %q · %s; what is typed there runs inside the container (terminal_read reads it)",
			svcShellLine(r.cmd.Argv), r.cmd.Label, r.h.contextLabel(ctx)), nil, nil
	})
}

// aiExecFiles lists, reads or uploads through the container's files.
func aiExecFiles(ctx context.Context, r aiExecReq, fsys workspace.FS) (string, error) {
	name := containerShownName(r.c)
	switch r.op {
	case "files":
		ents, err := containerhost.BrowseList(ctx, fsys, r.path)
		if err != nil {
			return "", fmt.Errorf("cannot list %s: %s", r.path, errLine(aiContainersRedact(err.Error())))
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Files of %s in %s: %d\n", name, r.path, len(ents))
		for i, e := range ents {
			if i == aiContainersListLimit {
				fmt.Fprintf(&b, "… %d more\n", len(ents)-i)
				break
			}
			switch {
			case e.Dir:
				b.WriteString("- " + e.Name + "/\n")
			case e.Link:
				b.WriteString("- " + e.Name + " (link)\n")
			default:
				fmt.Fprintf(&b, "- %s · %s\n", e.Name, humanBytes(e.Size))
			}
		}
		return aiContainersRedact(strings.TrimRight(b.String(), "\n")), nil
	case "read":
		f, err := containerhost.BrowseView(ctx, fsys, r.path, containerFileViewMax)
		if err != nil {
			return "", fmt.Errorf("cannot read %s: %s", r.path, errLine(aiContainersRedact(err.Error())))
		}
		if f.Binary {
			return "", fmt.Errorf("%s is a binary file (%s)", r.path, humanBytes(f.Size))
		}
		text, n := aiServicesRedact(f.Text)
		head := fmt.Sprintf("%s:%s (%s, %d redacted values)", name, f.Path, humanBytes(f.Size), n)
		if f.Truncated {
			head += fmt.Sprintf(" · the first %s only", humanBytes(containerFileViewMax))
		}
		return head + "\n" + aiCap(text, loadServicesSettings().MaxInspectKB, false), nil
	}
	target, err := containerhost.BrowseUpload(ctx, fsys, r.local, r.path, r.replace)
	if errors.Is(err, containerhost.ErrBrowseExists) {
		return "", fmt.Errorf("%s exists in %s; pass replace: true to replace it (it asks again)", path.Join(r.path, filepath.Base(r.local)), name)
	}
	if err != nil {
		return "", fmt.Errorf("cannot upload %s: %s", filepath.Base(r.local), errLine(aiContainersRedact(err.Error())))
	}
	return "uploaded " + filepath.Base(r.local) + " to " + name + ":" + target, nil
}

// aiContainersExecTool is containers_exec.
func (m *Model) aiContainersExecTool(call aiIDECaller) ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	session := aiContainersGate(m.services)
	return ai.Tool{
		Name: "containers_exec",
		Description: "Work inside a running container of a host, as the Containers view does. op shell (shell: one word like zsh; user, workdir), " +
			"attach and exec (command: a list like [\"ls\",\"-la\",\"/app\"]; user, workdir) open a terminal tab in the IDE (terminal_read reads it); " +
			"debug starts a toolbox container (image, default busybox) on its processes and network in a tab; files lists a folder (path), " +
			"read reads a text file (path); upload copies a workspace file (file) into a folder (path; replace: true over an existing one); " +
			"pair_sync starts Pair Sync of a container folder (path) with the workspace, whose card lists every change for the user. " +
			"exec asks; debug and upload ask the user every time; the rest run at once.",
		Params: map[string]any{
			"op":      map[string]any{"type": "string", "enum": aiExecOps, "description": "what to do"},
			"target":  str("container name or id (see containers_list)"),
			"context": aiContextParam(),
			"command": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "exec: the command and its arguments"},
			"shell":   str("shell: a shell like zsh (default bash, else sh)"),
			"user":    str("shell / exec: a user name or uid"), "workdir": str("shell / exec: an absolute folder to start in"),
			"image":   str("debug: the toolbox image (default busybox:stable)"),
			"path":    str("files / read / upload / pair_sync: an absolute path in the container"),
			"file":    str("upload: the file, relative to the workspace"),
			"replace": map[string]any{"type": "boolean", "description": "upload: replace a file of the same name"},
		},
		Required: []string{"op", "target"},
		Mutating: true,
		Gate: func(a map[string]any) (bool, string) {
			switch ai.ArgString(a, "op") {
			case "shell", "attach", "files", "read", "pair_sync":
				return false, ""
			case "exec":
				return session(a)
			}
			return true, ""
		},
		Validate: func(a map[string]any) error {
			ctx, cancel := context.WithTimeout(context.Background(), aiContainersCallWait)
			defer cancel()
			_, err := aiExecParse(ctx, call, a)
			return err
		},
		Describe: func(a map[string]any) string { return aiExecDescribe(call, a) },
		Run:      func(ctx context.Context, a map[string]any) (string, error) { return aiExecRun(ctx, call, a) },
	}
}
