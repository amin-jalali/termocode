package app

import (
	"bytes"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/aimemory"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
)

// AI: Memory… (OM-2): the lessons of this workspace, newest first. A row
// opens the lesson's card (Close, Open in editor, Forget…); Forget asks
// first with Keep focused. The last row forgets every lesson, after the
// same kind of card. Only the user runs it: the AI has no memory tool.

const confirmKindSharedMemory confirmKindEnum = 2652
const aiSharedMemoryID = "\x00shared-memory"

const pickerKindAIMemory pickerKindEnum = 1005

const (
	confirmKindAIMemoryForget    confirmKindEnum = 1004
	confirmKindAIMemoryLesson    confirmKindEnum = 1006
	confirmKindAIMemoryForgetAll confirmKindEnum = 1007
)

// aiMemoryForgetAllID is the picker's last row.
const aiMemoryForgetAllID = "\x00forget-all"

// openAIMemory is AI: Memory…: the picker, or a toast when there are no
// lessons.
func (m *Model) openAIMemory() tea.Cmd {
	s := m.aiS()
	ls, err := s.aiMemoryLoad(aiWorkspaceRoot())
	if err != nil {
		return m.aiToastErr("Could not read "+aiMemoryRel, err)
	}
	var shared []aimemory.Lesson
	if root := aimemory.SharedRoot(aiWorkspaceRoot()); root != "" {
		shared, _ = aimemory.LoadForWorktree(root)
	}
	if len(ls) == 0 && len(shared) == 0 {
		if aiMemoryState() == aiMemoryOn {
			return m.aiInfo("No lessons in this workspace yet")
		}
		return m.aiInfo("No lessons in this workspace · memory is off (AI: Memory: Turn On/Off)")
	}
	items := make([]picker.Item, 0, len(ls)+1)
	for i := len(ls) - 1; i >= 0; i-- {
		l := ls[i]
		items = append(items, picker.Item{ID: l.ID, Title: l.Date + " · " + aimemory.CleanLine(l.Title, aimemory.MaxTitleRunes),
			Hint: strings.Join(l.Paths, ", ")})
	}
	if len(shared) > 0 {
		items = append(items, picker.Item{ID: aiSharedMemoryID, Title: "Main checkout lessons (read only)", Hint: strconv.Itoa(len(shared)) + " shared lessons"})
	}
	if len(ls) > 0 {
		items = append(items, picker.Item{ID: aiMemoryForgetAllID, Title: "Forget all…"})
	}
	m.picker = picker.NewItems(" "+aiSpark()+" AI Memory ", items)
	state := "on"
	if aiMemoryState() != aiMemoryOn {
		state = "off"
	}
	m.picker.SetNote([]string{strconv.Itoa(len(ls)) + " lesson" + plural(len(ls)) + " in " + aiMemoryRel + " · memory is " + state})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIMemory
	return nil
}

// aiMemoryFind is the lesson id of this workspace.
func (s *aiState) aiMemoryFind(id string) (aimemory.Lesson, bool) {
	ls, _ := s.aiMemoryLoad(aiWorkspaceRoot())
	for _, l := range ls {
		if l.ID == id {
			return l, true
		}
	}
	return aimemory.Lesson{}, false
}

// onAIMemoryPicked opens the lesson's card (or the Forget all card).
func (m *Model) onAIMemoryPicked(id string) tea.Cmd {
	if id == aiSharedMemoryID {
		root := aimemory.SharedRoot(aiWorkspaceRoot())
		if root == "" {
			return nil
		}
		ls, _ := aimemory.LoadForWorktree(root)
		body := "Shared from the main checkout. Edit or forget these lessons there.\n"
		for _, l := range ls {
			body += "\n" + aimemory.CleanLine(l.Title, aimemory.MaxTitleRunes) + "\n" + aimemory.CleanLine(l.Text, aimemory.MaxTextRunes) + "\n"
		}
		m.confirm = confirm.New("Main checkout lessons", ai.Redact(body), []confirm.Button{{ID: "close", Title: "Close"}}).WithScrollText().WithWideText()
		m.confirmKind, m.confirmOpen = confirmKindSharedMemory, true
		m.confirm.SetSize(m.w, m.h)
		return nil
	}
	s := m.aiS()
	if id == aiMemoryForgetAllID {
		ls, _ := s.aiMemoryLoad(aiWorkspaceRoot())
		m.confirm = confirm.Risky("Forget all "+strconv.Itoa(len(ls))+" lesson"+plural(len(ls))+"?",
			"Every lesson in "+aiMemoryRel+" is removed. This cannot be undone (unless the file is in git).",
			confirm.Button{ID: "keep", Title: "Keep"},
			confirm.Button{ID: "forget", Title: "Forget all", Style: confirm.StyleDestructive})
		return m.openAIMemoryCard(confirmKindAIMemoryForgetAll, "")
	}
	l, ok := s.aiMemoryFind(id)
	if !ok {
		return nil
	}
	body := l.Date + "\n\n" + aimemory.CleanLine(l.Text, aimemory.MaxTextRunes*2)
	if len(l.Paths) > 0 {
		body += "\n\npaths: " + strings.Join(l.Paths, ", ")
	}
	m.confirm = confirm.New(aimemory.CleanLine(l.Title, aimemory.MaxTitleRunes), body, []confirm.Button{
		{ID: "close", Title: "Close"},
		{ID: "open", Title: "Open in editor"},
		{ID: "forget", Title: "Forget…"},
	})
	return m.openAIMemoryCard(confirmKindAIMemoryLesson, id)
}

func (m *Model) openAIMemoryCard(kind confirmKindEnum, id string) tea.Cmd {
	m.aiS().memLesson = id
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = kind
	return nil
}

// answerAIMemoryCard handles the lesson, Forget and Forget all cards ("" is
// Esc: nothing changes).
func (m *Model) answerAIMemoryCard(kind confirmKindEnum, id string) tea.Cmd {
	s := m.aiS()
	lesson := s.memLesson
	switch {
	case kind == confirmKindAIMemoryLesson && id == "open":
		return m.openAIMemoryFile(lesson)
	case kind == confirmKindAIMemoryLesson && id == "forget":
		l, ok := s.aiMemoryFind(lesson)
		if !ok {
			return nil
		}
		m.confirm = confirm.Risky("Forget this lesson?", aimemory.CleanLine(l.Title, aimemory.MaxTitleRunes)+
			"\n\nIt is removed from "+aiMemoryRel+"; later chats no longer get it.",
			confirm.Button{ID: "keep", Title: "Keep"},
			confirm.Button{ID: "forget", Title: "Forget", Style: confirm.StyleDestructive})
		return m.openAIMemoryCard(confirmKindAIMemoryForget, lesson)
	case kind == confirmKindAIMemoryForget && id == "forget":
		return m.forgetAIMemory(func(ls []aimemory.Lesson) []aimemory.Lesson { return aimemory.Forget(ls, lesson) }, "Lesson forgotten")
	case kind == confirmKindAIMemoryForgetAll && id == "forget":
		return m.forgetAIMemory(func([]aimemory.Lesson) []aimemory.Lesson { return nil }, "All lessons forgotten")
	}
	s.memLesson = ""
	return nil
}

// forgetAIMemory rewrites lessons.md with keep(lessons).
func (m *Model) forgetAIMemory(keep func([]aimemory.Lesson) []aimemory.Lesson, done string) tea.Cmd {
	s := m.aiS()
	s.memLesson = ""
	root := aiWorkspaceRoot()
	if s.memFake != nil {
		*s.memFake = keep(*s.memFake)
		return m.aiInfo(done)
	}
	aiMemoryWriteMu.Lock()
	defer aiMemoryWriteMu.Unlock()
	ls, err := aimemory.Load(root)
	if err == nil {
		err = aimemory.Save(root, keep(ls))
	}
	if err != nil {
		return m.aiToastErr("Could not change "+aiMemoryRel, err)
	}
	return m.aiInfo(done)
}

// openAIMemoryFile opens lessons.md in the editor at the lesson's heading.
func (m *Model) openAIMemoryFile(id string) tea.Cmd {
	m.aiS().memLesson = ""
	path := aimemory.Path(aiWorkspaceRoot())
	line := 1
	if b, err := os.ReadFile(path); err == nil && id != "" {
		if i := bytes.Index(b, []byte("{#"+id+"}")); i >= 0 {
			line = bytes.Count(b[:i], []byte("\n")) + 1
		}
	}
	m.openFileAtLine(path, line)
	return nil
}
