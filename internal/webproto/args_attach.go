package webproto

import (
	"encoding/json"
	"strings"
)

// Attach is one item the user sends back with a chat message (MOB-5,
// plan §3.3): a reference, never the item's content. The laptop resolves
// it under the same secret rules as every op (a secret-like file stays
// on the laptop) and gives the AI what it found; the message shows one
// chip per attach (ChatMsg.Attached).
//
// What names the item, by kind:
//
//	file             Path
//	lines            Path, From..To (1-based, inclusive)
//	diff             Path ("" is the whole working tree)
//	ptask            Path (the checklist file) and ID (the task's line key)
//	task             ID: the label of a laptop task (tasks topic)
//	run              ID: a run key (runs topic)
//	worktree         ID: a worktree id (worktrees topic)
//	workflow         ID: a workflow name (workflows topic)
//	card             ID: a card id (cards topic)
//	issue, pr        ID: its number (or a GitLab "!12")
//	race, block,
//	project          ID
//	resource         ID: the MCP resource URI
//	view             Spec: the view spec as JSON (ID: the view id)
//
// Label is the chip's text the page showed; the laptop may shorten it.
type Attach struct {
	Kind  string `json:"kind" enum:"task|ptask|issue|pr|file|lines|diff|worktree|run|race|workflow|card|resource|block|project|view"`
	ID    string `json:"id,omitempty"`
	Path  string `json:"path,omitempty"`
	From  int    `json:"from,omitempty"`
	To    int    `json:"to,omitempty"`
	Label string `json:"label,omitempty"`
	Spec  string `json:"spec,omitempty"`
}

// The attach kinds.
const (
	AttachTask     = "task"
	AttachPTask    = "ptask"
	AttachIssue    = "issue"
	AttachPR       = "pr"
	AttachFile     = "file"
	AttachLines    = "lines"
	AttachDiff     = "diff"
	AttachWorktree = "worktree"
	AttachRun      = "run"
	AttachRace     = "race"
	AttachWorkflow = "workflow"
	AttachCard     = "card"
	AttachResource = "resource"
	AttachBlock    = "block"
	AttachProject  = "project"
	AttachView     = "view"
)

// AttachKinds lists the kinds in the order of the enum.
func AttachKinds() []string {
	return []string{AttachTask, AttachPTask, AttachIssue, AttachPR, AttachFile, AttachLines, AttachDiff, AttachWorktree,
		AttachRun, AttachRace, AttachWorkflow, AttachCard, AttachResource, AttachBlock, AttachProject, AttachView}
}

// Limits of attaches (plan §3.3). The byte caps are for what the laptop
// resolves, not for the request: a resolved attach is cut at
// MaxAttachBytes and the whole message's at MaxAttachTotal.
const (
	MaxAttach        = 8
	MaxAttachLabel   = 120
	MaxAttachURI     = 1024
	MaxAttachSpec    = 4 << 10
	MaxAttachLines   = 2000 // lines of one lines attach
	MaxAttachBytes   = 32 << 10
	MaxAttachTotal   = 96 << 10
	MaxAttachedChips = MaxAttach // ChatMsg.Attached
)

// AttachChip is one attach as a message shows it: its kind and label.
type AttachChip struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

func validAttaches(list []Attach) error {
	if len(list) > MaxAttach {
		return invalid("%d attaches", len(list))
	}
	for i := range list {
		if err := list[i].check(); err != nil {
			return err
		}
	}
	return nil
}

// check refuses an attach whose fields do not fit its kind.
func (a *Attach) check() error {
	if a.Label != "" && !validName(a.Label, MaxAttachLabel) {
		return invalid("bad attach label")
	}
	if a.From < 0 || a.To < 0 {
		return invalid("bad attach lines")
	}
	noPath := a.Path == ""
	noLines := a.From == 0 && a.To == 0
	switch a.Kind {
	case AttachFile:
		if !validPath(a.Path, false) || a.ID != "" || !noLines || a.Spec != "" {
			return invalid("bad file attach")
		}
	case AttachLines:
		if !validPath(a.Path, false) || a.ID != "" || a.Spec != "" || a.From < 1 || a.To < a.From || a.To-a.From >= MaxAttachLines {
			return invalid("bad lines attach")
		}
	case AttachDiff:
		if !validPath(a.Path, true) || a.ID != "" || !noLines || a.Spec != "" {
			return invalid("bad diff attach")
		}
	case AttachPTask:
		if !validPath(a.Path, false) || !validName(a.ID, MaxLabel) || !noLines || a.Spec != "" {
			return invalid("bad project task attach")
		}
	case AttachResource:
		if !validName(a.ID, MaxAttachURI) || !noPath || !noLines || a.Spec != "" {
			return invalid("bad resource attach")
		}
	case AttachView:
		if (a.ID != "" && !validName(a.ID, MaxID)) || !noPath || !noLines || !validAttachSpec(a.Spec) {
			return invalid("bad view attach")
		}
	case AttachTask:
		if !validName(a.ID, MaxLabel) || !noPath || !noLines || a.Spec != "" {
			return invalid("bad task attach")
		}
	case AttachIssue, AttachPR, AttachWorktree, AttachRun, AttachRace, AttachWorkflow, AttachCard, AttachBlock, AttachProject:
		if !validName(a.ID, MaxID) || !noPath || !noLines || a.Spec != "" {
			return invalid("bad %s attach", a.Kind)
		}
	default:
		return invalid("unknown attach kind")
	}
	return nil
}

// validAttachSpec accepts one JSON object of at most MaxAttachSpec bytes.
func validAttachSpec(s string) bool {
	if s == "" || len(s) > MaxAttachSpec || !strings.HasPrefix(strings.TrimSpace(s), "{") {
		return false
	}
	var v map[string]any
	return json.Unmarshal([]byte(s), &v) == nil
}
