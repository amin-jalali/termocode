package app

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// CTX-34: run-from-image "More options…", and Duplicate / Recreate on a
// container row. All three show the exact command on a Cancel-first card
// and run it in a visible tab. Env values never go into argv: the command
// names them (-e NAME) and the tab's environment carries the values.

const (
	promptKindContainerRunOptions promptKindEnum  = 5342
	confirmKindContainerClone     confirmKindEnum = 5342
)

// clonePending is the Duplicate / Recreate card's plan.
type clonePending struct {
	op, name, line string
	env            map[string]string
}

// containerCloneMsg carries the inspected settings of a container.
type containerCloneMsg struct {
	op, id string
	opts   containers.RunOptions
	err    error
}

// parseRunOptionsLine reads the More options words into o:
// volume=SRC:DST network= restart= user= workdir= hostname= platform=
// label=K=V entrypoint= detach rm no-rm, then `--` and the command.
func parseRunOptionsLine(line string, o *containers.RunOptions) error {
	words, err := svcShellWords(line)
	if err != nil {
		return err
	}
	for i, w := range words {
		if w == "--" {
			o.Command = append([]string(nil), words[i+1:]...)
			return nil
		}
		switch w {
		case "detach":
			o.Detach = true
			continue
		case "rm":
			o.Remove = true
			continue
		case "no-rm":
			o.Remove = false
			continue
		}
		k, v, ok := strings.Cut(w, "=")
		if !ok || v == "" {
			return errors.New("write option=value (for example network=back), or detach, rm, no-rm")
		}
		switch k {
		case "volume":
			o.Volumes = append(o.Volumes, v)
		case "network":
			o.Network = v
		case "restart":
			o.Restart = v
		case "user":
			o.User = v
		case "workdir":
			o.Workdir = v
		case "hostname":
			o.Hostname = v
		case "platform":
			o.Platform = v
		case "entrypoint":
			o.Entrypoint = v
		case "label":
			lk, lv, ok := strings.Cut(v, "=")
			if !ok || lk == "" {
				return errors.New("write label=KEY=value")
			}
			if o.Labels == nil {
				o.Labels = map[string]string{}
			}
			o.Labels[lk] = lv
		default:
			return errors.New("unknown option " + k)
		}
	}
	return nil
}

// openRunImageCard is the run-from-image card for args.
func (m *Model) openRunImageCard(args []string) {
	m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = args, "run-image", "images"
	m.confirm = confirm.New("Run image?", "Review the command before it runs.\n\nCommand: "+svcShellLine(args), []confirm.Button{
		{ID: "cancel", Title: "Cancel"}, {ID: "run-options", Title: "More options…"}, {ID: "run", Title: "Run", Style: confirm.StylePrimary},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
	m.promptOpen = false
}

// askRunOptions is "More options…" on the run card.
func (m *Model) askRunOptions() tea.Cmd {
	m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = nil, "", ""
	m.prompt = prompt.New("Run image: more options",
		"volume=SRC:DST network= restart= user= workdir= label=K=V entrypoint= detach no-rm -- command…", "")
	m.prompt.SetAllowEmpty(true)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindContainerRunOptions
	return nil
}

// runImageBaseOptions are the name, ports and env typed so far.
func (m *Model) runImageBaseOptions() containers.RunOptions {
	o := containers.RunOptions{Image: m.pendingImageRef, Name: m.pendingRunName, Remove: true,
		Ports: append([]string(nil), m.pendingRunPorts...)}
	for _, kv := range m.pendingRunEnv {
		k, v, _ := strings.Cut(kv, "=")
		if o.Env == nil {
			o.Env = map[string]string{}
		}
		o.Env[k] = v
	}
	return o
}

// onRunOptionsPrompt builds the full command and shows the card again.
func (m *Model) onRunOptionsPrompt(value string) tea.Cmd {
	o := m.runImageBaseOptions()
	if err := parseRunOptionsLine(value, &o); err != nil {
		return m.servicesToast(toast.Info, "Options not used", err.Error())
	}
	cmd, err := m.services.containerEngine.RunOptionsCommand(o)
	if err != nil {
		return m.servicesToast(toast.Info, "Options not used", err.Error())
	}
	m.services.runEnv = cmd.Env
	m.openRunImageCard(cmd.Argv)
	return nil
}

// containerCloneStart inspects c for Duplicate or Recreate.
func (m *Model) containerCloneStart(op string, c containers.Container) tea.Cmd {
	e := m.services.containerEngine
	if e.Binary == "" {
		return m.servicesToast(toast.Errr, "No container engine", "")
	}
	id := c.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		b, err := e.InspectRaw(ctx, id)
		if err != nil {
			return containerCloneMsg{op: op, id: id, err: err}
		}
		o, err := containers.RunOptionsFromInspect(b)
		return containerCloneMsg{op: op, id: id, opts: o, err: err}
	}
}

// onContainerClone shows the Duplicate or Recreate card.
func (m *Model) onContainerClone(msg containerCloneMsg) tea.Cmd {
	st := m.services
	what := map[string]string{"duplicate": "duplicate", "recreate": "recreate"}[msg.op]
	if what == "" {
		return nil
	}
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot "+what+" the container", errLine(msg.err.Error()))
	}
	o := msg.opts
	o.Detach = true
	name := sanitizeOutputLine(o.Name)
	e := st.containerEngine
	var p clonePending
	var title, body string
	var go_ confirm.Button
	if msg.op == "duplicate" {
		var taken []string
		for _, c := range st.containerRows {
			taken = append(taken, c.Names...)
		}
		if o.Name != "" {
			o.Name = containers.DuplicateName(o.Name, taken)
		}
		cmd, err := e.RunOptionsCommand(o)
		if err != nil {
			return m.servicesToast(toast.Errr, "Cannot duplicate the container", err.Error())
		}
		p = clonePending{op: "duplicate", name: o.Name, line: svcShellLine(cmd.Argv), env: cmd.Env}
		title = "Duplicate " + name + "?"
		body = "Starts a new container with the same image and settings. Env values are passed by name and not shown."
		if len(o.Ports) > 0 {
			body += " The host ports are the same: the run fails while " + name + " still uses them."
		}
		go_ = confirm.Button{ID: "duplicate", Title: "Duplicate", Style: confirm.StylePrimary}
	} else {
		steps, env, err := e.RecreatePlan(msg.id, o)
		if err != nil {
			return m.servicesToast(toast.Errr, "Cannot recreate the container", err.Error())
		}
		parts := make([]string, len(steps))
		for i, s := range steps {
			parts[i] = svcShellLine(s)
		}
		p = clonePending{op: "recreate", name: o.Name, line: strings.Join(parts, " && "), env: env}
		title = "Recreate " + name + "?"
		body = "Pulls the image again, removes this container (changes inside it are lost; volumes stay) and starts a new one with the same settings."
		go_ = confirm.Button{ID: "recreate", Title: "Recreate", Style: confirm.StyleDestructive}
	}
	st.clonePending = p
	m.confirm = confirm.New(title, body+"\n\nCommand: "+p.line, []confirm.Button{{ID: "cancel", Title: "Cancel"}, go_})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerClone
	return nil
}

// answerContainerClone runs the plan in a tab on Duplicate / Recreate.
func (m *Model) answerContainerClone(id string) tea.Cmd {
	st := m.services
	p := st.clonePending
	st.clonePending = clonePending{}
	if p.line == "" || id != p.op {
		return nil
	}
	env := map[string]any{}
	for k, v := range st.contextEnv() {
		env[k] = v
	}
	for k, v := range p.env {
		env[k] = v
	}
	job := terminalJob{Label: p.op + ": " + p.name, Cmdline: p.line, Env: env, Service: containerTabPrefix + p.op + ":" + p.name}
	if _, err := m.spawnTerminalJob(job); err != nil {
		return m.servicesToast(toast.Errr, "Cannot "+p.op+" the container", err.Error())
	}
	return nil
}
