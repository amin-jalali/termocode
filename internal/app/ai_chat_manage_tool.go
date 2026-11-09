package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// chat_manage: the AI renames a saved chat and deletes one, as AI: Rename
// Chat… and Delete Chat… (and the web's chat.rename and chat.delete) do.
// Rename has no card; Delete asks every time, as the GUI's card does
// (Cancel first). The chat the AI answers in is never deleted here.
// chats_search finds the ids; chat_read reads (and exports) one.

func aiChatManageCheck(a map[string]any) error {
	switch ai.ArgString(a, "op") {
	case "rename":
		if t := strings.TrimSpace(ai.ArgString(a, "title")); t == "" || len([]rune(t)) > 120 {
			return errors.New("give title: up to 120 characters")
		}
	case "delete":
	default:
		return errors.New("give op: rename or delete")
	}
	if strings.TrimSpace(ai.ArgString(a, "id")) == "" {
		return errors.New("give id: a chat id (chats_search shows them)")
	}
	return nil
}

func aiChatManageTool(call aiIDECaller) ai.Tool {
	return ai.Tool{
		Name:        "chat_manage",
		Description: "Rename a saved AI chat (at once) or delete one (asks the user every time). chats_search finds the ids; chat_read reads one.",
		Params: map[string]any{
			"op":    map[string]any{"type": "string", "enum": []string{"delete", "rename"}, "description": "what to do"},
			"id":    map[string]any{"type": "string", "description": "the chat id"},
			"title": map[string]any{"type": "string", "description": "rename: the new title"},
		},
		Required: []string{"op", "id"},
		Validate: aiChatManageCheck,
		Describe: func(a map[string]any) string {
			if ai.ArgString(a, "op") == "delete" {
				return "Delete the chat " + aiCardText(ai.ArgString(a, "id"), 40) + "? This cannot be undone."
			}
			return fmt.Sprintf("Rename the chat %s to %q", aiCardText(ai.ArgString(a, "id"), 40), strings.TrimSpace(ai.ArgString(a, "title")))
		},
		Gate: func(a map[string]any) (bool, string) { return ai.ArgString(a, "op") != "rename", "" },
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			if err := aiChatManageCheck(a); err != nil {
				return "", err
			}
			if call == nil {
				return "", errors.New("the chats are on the laptop")
			}
			op, id, title := ai.ArgString(a, "op"), strings.TrimSpace(ai.ArgString(a, "id")), strings.TrimSpace(ai.ArgString(a, "title"))
			return call(ctx, func(m *Model) (string, tea.Cmd, error) {
				s := m.aiS()
				if op == "rename" && id == s.conv.ID {
					s.conv.Title = title
					if len(s.conv.Messages) > 0 && !s.streaming {
						if err := s.store.Save(&s.conv); err != nil {
							return "", nil, errors.New("could not rename the chat")
						}
					}
					return fmt.Sprintf("This chat is now %q.", title), nil, nil
				}
				if id == s.conv.ID {
					return "", nil, errors.New("this is the chat you answer in; the user deletes it")
				}
				c, err := s.store.Load(id)
				if err != nil || c.ID != id {
					return "", nil, errors.New("no such chat (chats_search shows them)")
				}
				if op == "rename" {
					c.Title = title
					if err := s.store.Save(&c); err != nil {
						return "", nil, errors.New("could not rename the chat")
					}
					return fmt.Sprintf("Renamed the chat to %q.", title), nil, nil
				}
				if err := s.store.Delete(id); err != nil {
					return "", nil, errors.New("could not delete the chat")
				}
				m.dropOpenChatIf(func(c string) bool { return c == id })
				return fmt.Sprintf("Deleted the chat %q.", webChatTitle(c)), nil, nil
			})
		},
	}
}
