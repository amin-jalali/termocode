package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/aimemory"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Memory (OM-2): short lessons the AI wrote after earlier turns in this
// workspace, in .termocode/memory/lessons.md (ignored by git). Memory is
// off until the user turns it on for the workspace; then the lessons that
// fit a turn go into its system prompt after the project rules. The AI
// has no memory tool: only the user turns memory on or off and forgets
// lessons. Lesson text is never logged.

// Memory states in the session file (SessionState.AIMemory).
const (
	aiMemoryUndecided = ""
	aiMemoryOn        = "on"
	aiMemoryOff       = "off"
	// aiMemoryRead is "use only" (CMP-73): saved lessons go into prompts
	// and recall, but the extractor writes no new ones.
	aiMemoryRead = "read"
)

// aiMemoryUses says whether saved lessons are used (on or use only).
func aiMemoryUses(state string) bool { return state == aiMemoryOn || state == aiMemoryRead }

// aiMemoryLearns says whether new lessons are written (on only).
func aiMemoryLearns(state string) bool { return state == aiMemoryOn }

// aiMemoryLevelText is the level for notes and toasts.
func aiMemoryLevelText(state string) string {
	switch state {
	case aiMemoryOn:
		return "on (use and learn)"
	case aiMemoryRead:
		return "on (use only)"
	}
	return "off"
}

// aiMemoryKindTag is the prompt tag of a lesson kind ("" for a plain
// lesson).
func aiMemoryKindTag(kind string) string {
	if k := aimemory.NormKind(kind); k != "" {
		return "[" + k + "] "
	}
	return ""
}

// aiMemoryPromptBytes and aiMemoryPromptMax cap the lessons of one turn.
const (
	aiMemoryPromptBytes = 2 << 10
	aiMemoryPromptMax   = 10
)

// aiMemoryState is the memory switch of this workspace.
func aiMemoryState() string { return loadSession().AIMemory }

// aiMemoryRel is the lessons file relative to the workspace (for text).
const aiMemoryRel = aimemory.DirRel + "/" + aimemory.File

// aiMemoryLoad reads the lessons of root (the demo's fake list when set).
func (s *aiState) aiMemoryLoad(root string) ([]aimemory.Lesson, error) {
	if s.memFake != nil {
		return *s.memFake, nil
	}
	return aimemory.Load(root)
}

// aiMemoryTurn is the prompt block of the lessons that fit a turn about
// text (and the open and attached files), and how many there are. ""
// and 0 while memory is off or there are no lessons. Lessons are redacted
// on the way in: a hand-edited file may hold a secret.
func (m *Model) aiMemoryTurn(root, text string) (string, int) {
	s := m.aiS()
	if !aiMemoryUses(aiMemoryState()) {
		return "", 0
	}
	var ls []aimemory.Lesson
	var err error
	if s.memFake != nil {
		ls = *s.memFake
	} else {
		ls, err = aimemory.LoadForWorktree(root)
	}
	if err != nil || len(ls) == 0 {
		return "", 0
	}
	h := aimemory.Hints{Text: text}
	if p := m.editor.Path(); p != "" {
		h.Paths = append(h.Paths, p)
	}
	for _, a := range s.attachments {
		if a.Image != "" {
			h.Paths = append(h.Paths, a.Image)
		}
		if f, _, _ := strings.Cut(a.Label, " "); f != "" {
			f, _, _ = strings.Cut(f, ":")
			h.Paths = append(h.Paths, f)
		}
	}
	sel := aimemory.Select(ls, h, aiMemoryPromptBytes, aiMemoryPromptMax)
	if len(sel) == 0 {
		return "", 0
	}
	var b strings.Builder
	b.WriteString("\n\nLessons you wrote in earlier sessions in this repository (your own notes; use them when they fit, never over the user's request):")
	for _, l := range sel {
		line := "\n- " + aiMemoryKindTag(l.Kind) + aimemory.CleanLine(l.Title, aimemory.MaxTitleRunes) + ": " + aimemory.CleanLine(l.Text, aimemory.MaxTextRunes)
		if len(l.Paths) > 0 {
			line += " (" + strings.Join(l.Paths, ", ") + ")"
		}
		b.WriteString(ai.Redact(line))
	}
	return b.String(), len(sel)
}

// aiMemoryLabel is the memory part of the rules line under a user
// message ("memory: 3 lessons").
func aiMemoryLabel(n int) string {
	if n <= 0 {
		return ""
	}
	return "memory: " + strconv.Itoa(n) + " lesson" + plural(n)
}

// toggleAIMemory is AI: Memory: Turn On/Off.
func (m *Model) toggleAIMemory() tea.Cmd {
	m.dropAIMemoryAsk()
	if aiMemoryUses(aiMemoryState()) {
		sess := loadSession()
		sess.AIMemory = aiMemoryOff
		saveSession(sess)
		return m.aiInfo("Memory is off in this workspace (saved lessons stay in " + aiMemoryRel + ")")
	}
	return m.turnOnAIMemory()
}

// turnOnAIMemory turns memory on for this workspace and makes git ignore
// the memory folder; the toast says where lessons go and what changed in
// .gitignore.
func (m *Model) turnOnAIMemory() tea.Cmd { return m.turnOnAIMemoryLevel(aiMemoryOn) }

// toggleAIMemoryLearn is AI: Memory: Learn New Lessons On/Off (CMP-73):
// "use and learn" ⇄ "use only". From off it turns memory on, use only.
func (m *Model) toggleAIMemoryLearn() tea.Cmd {
	switch was := aiMemoryState(); was {
	case aiMemoryOn, aiMemoryRead:
		m.dropAIMemoryAsk()
		sess := loadSession()
		sess.AIMemory = aiMemoryRead
		if was == aiMemoryRead {
			sess.AIMemory = aiMemoryOn
		}
		saveSession(sess)
		if sess.AIMemory == aiMemoryOn {
			return m.aiInfo("Memory: use and learn · new lessons go to " + aiMemoryRel)
		}
		return m.aiInfo("Memory: use only · saved lessons are used, no new ones are written")
	}
	return m.turnOnAIMemoryLevel(aiMemoryRead)
}

// turnOnAIMemoryLevel turns memory on at level (on or read).
func (m *Model) turnOnAIMemoryLevel(level string) tea.Cmd {
	m.dropAIMemoryAsk()
	sess := loadSession()
	sess.AIMemory = level
	saveSession(sess)
	var detail string
	switch res, err := aimemory.EnsureIgnored(aiWorkspaceRoot()); {
	case err != nil:
		recordError("[ai] memory .gitignore: " + err.Error())
		detail = "Could not update .gitignore: add " + aimemory.GitignoreLine + " yourself"
	case res == aimemory.IgnoreAdded:
		detail = "termocode added " + aimemory.GitignoreLine + " to .gitignore"
	case res == aimemory.IgnoreCreated:
		detail = "termocode made .gitignore with " + aimemory.GitignoreLine
	default:
		detail = "The folder is already in .gitignore"
	}
	var c tea.Cmd
	title := "Memory is on · lessons go to " + aiMemoryRel
	if level == aiMemoryRead {
		title = "Memory is on, use only · lessons are read from " + aiMemoryRel
	}
	m.toast, c = m.toast.PushDetail(toast.Info, title, detail)
	return c
}

// dropAIMemoryAsk removes the ask-once card from the chat (the workspace
// decided another way).
func (m *Model) dropAIMemoryAsk() {
	if s := m.aiS(); s.memAsk != nil {
		s.memAsk = nil
		s.runGen++
	}
}
