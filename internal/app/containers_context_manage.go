package app

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── CONTEXTS: create, inspect, remove (CTX-24) ───────────────────────────
//
// n on CONTEXTS asks for a name, then the host (ssh://, tcp:// or
// unix://); a tcp host also asks for the TLS files (CA, cert, key paths;
// podman over ssh: the identity file). A card shows the exact command,
// Cancel first. i opens `context inspect` as read-only JSON. x removes a
// context after a card; the default and the one in use cannot go. Only
// file paths are passed: no key material is read or stored.

const (
	confirmKindContextManage confirmKindEnum = 5211
	promptKindContextName    promptKindEnum  = 5212
	promptKindContextHost    promptKindEnum  = 5213
	promptKindContextTLS     promptKindEnum  = 5214
)

type contextManageDoneMsg struct {
	verb, name string
	err        error
}

type contextInspectMsg struct {
	name, text string
	err        error
}

func (m *Model) openContextPrompt(kind promptKindEnum, label, initial, note string) {
	m.prompt = prompt.New("Create "+m.services.contextWord(), label, initial)
	if note != "" {
		m.prompt.SetNote(note)
	}
	if kind == promptKindContextTLS {
		m.prompt.SetAllowEmpty(true)
	}
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, kind
}

// askContextCreate starts the create steps.
func (m *Model) askContextCreate() tea.Cmd {
	st := m.services
	if st == nil || !st.containerDetected || st.containerEngine.Binary == "" {
		return m.servicesToast(toast.Info, "Container engine is not ready", "Refresh the Containers view and try again")
	}
	m.pendingContextSpec = containers.ContextSpec{}
	m.openContextPrompt(promptKindContextName, "Name (letters, digits, . - _):", "", "")
	return nil
}

// onContextPrompt takes one create step's answer.
func (m *Model) onContextPrompt(kind promptKindEnum, value string) tea.Cmd {
	st := m.services
	value = strings.TrimSpace(value)
	spec := m.pendingContextSpec
	fail := func(detail string) tea.Cmd {
		m.promptOpen = false
		m.pendingContextSpec = containers.ContextSpec{}
		return m.servicesToast(toast.Info, "Not created", detail)
	}
	switch kind {
	case promptKindContextName:
		spec.Name = value
		// The engine's own name check, with a placeholder host.
		if _, err := st.containerEngine.CreateContextArgs(containers.ContextSpec{Name: value, Host: "unix:///x"}); err != nil {
			return fail(err.Error())
		}
		for _, c := range st.contextRows {
			if c.Name == value {
				return fail(sanitizeOutputLine(value) + " already exists")
			}
		}
		m.pendingContextSpec = spec
		m.openContextPrompt(promptKindContextHost, "Host (ssh://user@host, tcp://host:2376 or unix:///path):", "ssh://", "")
	case promptKindContextHost:
		spec.Host = value
		if _, err := st.containerEngine.CreateContextArgs(spec); err != nil {
			return fail(err.Error())
		}
		m.pendingContextSpec = spec
		switch {
		case strings.HasPrefix(value, "tcp://") && !st.isPodman():
			m.openContextPrompt(promptKindContextTLS, "TLS files: CA cert key (paths, space between; empty for none):", "",
				"Only the paths are saved in the context; termocode does not read the files.")
		case strings.HasPrefix(value, "ssh://") && st.isPodman():
			m.openContextPrompt(promptKindContextTLS, "Identity file (empty for the ssh agent):", "", "")
		default:
			m.promptOpen = false
			return m.askContextCreateCard(spec)
		}
	case promptKindContextTLS:
		f := strings.Fields(value)
		if st.isPodman() {
			if len(f) > 1 {
				return fail("give one identity file path")
			}
			if len(f) == 1 {
				spec.Key = f[0]
			}
		} else {
			if len(f) != 0 && len(f) != 3 {
				return fail("give three paths (CA, cert, key) or none")
			}
			if len(f) == 3 {
				spec.CA, spec.Cert, spec.Key = f[0], f[1], f[2]
			}
		}
		m.promptOpen = false
		return m.askContextCreateCard(spec)
	}
	return nil
}

func (m *Model) askContextCreateCard(spec containers.ContextSpec) tea.Cmd {
	st := m.services
	args, err := st.containerEngine.CreateContextArgs(spec)
	if err != nil {
		m.pendingContextSpec = containers.ContextSpec{}
		return m.servicesToast(toast.Info, "Not created", err.Error())
	}
	word := st.contextWord()
	argv := append([]string{st.containerEngine.Binary}, args...)
	m.confirm = confirm.New("Create "+word+" "+sanitizeOutputLine(spec.Name)+"?",
		"This adds a "+word+" for every terminal on this machine. It does not switch to it: Enter on the row uses it here.\n\nCommand: "+svcShellLine(argv),
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "create", Title: "Create", Style: confirm.StylePrimary}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContextManage
	m.pendingContextSpec, m.pendingContextVerb = spec, "create"
	return nil
}

// askContextRemove is x on a context: a card with the exact command.
func (m *Model) askContextRemove(name string) tea.Cmd {
	st := m.services
	word := st.contextWord()
	if name == podmanLocal || name == "default" {
		return m.servicesToast(toast.Info, sanitizeOutputLine(name)+" cannot be removed", "It is the engine's own "+word)
	}
	for _, c := range st.contextRows {
		if c.Name == name && c.Current {
			return m.servicesToast(toast.Info, "This "+word+" is the default", "Make another one the default first (D)")
		}
	}
	if st.sessionContext == name {
		return m.servicesToast(toast.Info, "This session uses "+sanitizeOutputLine(name), "Switch to another "+word+" first (Enter)")
	}
	args, err := st.containerEngine.RemoveContextArgs(name)
	if err != nil {
		return m.servicesToast(toast.Info, "Cannot remove", err.Error())
	}
	argv := append([]string{st.containerEngine.Binary}, args...)
	m.confirm = confirm.New("Remove "+word+" "+sanitizeOutputLine(name)+"?",
		"This removes it for every terminal on this machine. The remote host and its containers are not touched.\n\nCommand: "+svcShellLine(argv),
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "remove", Title: "Remove", Style: confirm.StyleDestructive}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContextManage
	m.pendingContextSpec, m.pendingContextVerb = containers.ContextSpec{Name: name}, "remove"
	return nil
}

// runContextManage answers the create / remove card.
func (m *Model) runContextManage(id string) tea.Cmd {
	spec, verb := m.pendingContextSpec, m.pendingContextVerb
	m.pendingContextSpec, m.pendingContextVerb = containers.ContextSpec{}, ""
	if verb == "" || id != verb {
		return nil
	}
	e := m.services.containerEngine
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var err error
		if verb == "create" {
			err = e.CreateContext(ctx, spec)
		} else {
			err = e.RemoveContext(ctx, spec.Name)
		}
		return contextManageDoneMsg{verb: verb, name: spec.Name, err: err}
	}
}

func (m *Model) onContextManageDone(msg contextManageDoneMsg) tea.Cmd {
	word := m.services.contextWord()
	name := sanitizeOutputLine(msg.name)
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Could not "+msg.verb+" "+word+" "+name, sanitizeOutputLine(errLine(msg.err.Error())))
	}
	title := "Created " + word + " " + name
	detail := "Enter on it uses it in this session"
	if msg.verb == "remove" {
		title, detail = "Removed "+word+" "+name, ""
	}
	return tea.Batch(m.servicesToast(toast.Info, title, detail), m.containersRefreshContexts())
}

// inspectContext opens `context inspect` as a read-only page.
func (m *Model) inspectContext(name string) tea.Cmd {
	e := m.services.containerEngine
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		text, err := e.InspectContext(ctx, name)
		return contextInspectMsg{name: name, text: text, err: err}
	}
}

func (m *Model) onContextInspect(msg contextInspectMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Info, "Cannot inspect "+sanitizeOutputLine(msg.name), sanitizeOutputLine(errLine(msg.err.Error())))
	}
	if err := m.openReadOnlyPage("context: "+msg.name, msg.text, "", "json"); err != nil {
		return m.servicesToast(toast.Errr, "Cannot open the context page", err.Error())
	}
	return nil
}
