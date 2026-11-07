package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// ── Share a chat by link; comments from the person (CMP-88) ─────────────
//
// AI: Share Chat by Link… makes the viewer link of Web: Share as Viewer
// (web_access.go) after a card (Cancel first) that says what the person
// sees: the web panel read only, with the laptop's open chat. On the web
// a viewer gets a comment box under the chat instead of the composer
// (chat.comment). A comment is kept on the laptop beside the chat, in
// termocode's config folder (chat-comments/<chat id>.json, 0600), never
// in the chat's messages: the AI reads a comment only when the user adds
// it to a message (AI: Chat Comments…, Enter, Add to message).

const (
	aiShareChatID                            = "ai-share-chat"
	aiChatCommentsID                         = "ai-chat-comments"
	confirmKindAIShareChat   confirmKindEnum = 30311
	confirmKindAIChatComment confirmKindEnum = 30312
	pickerKindAIChatComments pickerKindEnum  = 30313
	aiChatCommentsKeep                       = 200 // comments kept per chat (the oldest go)
	aiChatCommentPfx                         = "cm:"
	aiChatCommentToastRunes                  = 80
)

// aiChatComment is one comment on a chat.
type aiChatComment struct {
	AtMs int64  `json:"at_ms"`
	Who  string `json:"who"` // the device's stored name, or "A web viewer"
	Text string `json:"text"`
}

// aiChatCommentsPath is the comments file of chat id ("" when the config
// folder is unknown or the id is not a plain name).
func aiChatCommentsPath(id string) string {
	if id == "" || strings.ContainsAny(id, `/\`) || id == "." || id == ".." {
		return ""
	}
	d, err := ai.ConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "chat-comments", id+".json")
}

// aiChatCommentsLoad reads chat id's comments, oldest first.
func aiChatCommentsLoad(id string) []aiChatComment {
	p := aiChatCommentsPath(id)
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var out []aiChatComment
	if json.Unmarshal(b, &out) != nil {
		recordError("[ai] chat comments: cannot read " + p)
		return nil
	}
	return out
}

// aiChatCommentsAdd appends c to chat id's comments (keeping the newest
// aiChatCommentsKeep) and writes the file through a temp file.
func aiChatCommentsAdd(id string, c aiChatComment) error {
	p := aiChatCommentsPath(id)
	if p == "" {
		return os.ErrInvalid
	}
	list := append(aiChatCommentsLoad(id), c)
	if n := len(list) - aiChatCommentsKeep; n > 0 {
		list = list[n:]
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// webConnName is the stored name of a paired device (never the name the
// browser sent), or a plain word.
func webConnName(c *webConn) string {
	if c != nil && strings.HasPrefix(c.device, "dev-") {
		if dir, err := webDevicesDir(); err == nil {
			if d, ok, _ := webdevices.Open(dir).Get(c.device); ok && d.Name != "" {
				return d.Name
			}
		}
		return "A paired device"
	}
	return "A web viewer"
}

func init() {
	webOps[webproto.OpChatComment] = (*Model).webChatComment
	aiActionPolicies[aiShareChatID] = aiNever("it makes a link that lets another person watch this computer; only the user shares")
	aiActionPolicies[aiChatCommentsID] = aiNever(aiNoPicker + "; a comment reaches the AI only when the user adds it to a message")
}

// webChatComment keeps a viewer's comment on a chat and tells the user.
func (m *Model) webChatComment(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ChatCommentArgs)
	s := m.aiS()
	title := ""
	if a.ID == s.conv.ID {
		title = s.conv.Title
	} else if c, err := s.store.Load(a.ID); err == nil && c.ID == a.ID {
		title = c.Title
	} else {
		call.fail(webproto.CodeNotFound, "That chat is gone.")
		return nil
	}
	who := webConnName(call.conn)
	text := strings.TrimSpace(a.Text)
	if err := aiChatCommentsAdd(a.ID, aiChatComment{AtMs: time.Now().UnixMilli(), Who: who, Text: text}); err != nil {
		recordError("[ai] chat comment: " + err.Error())
		call.fail(webproto.CodeInternal, "The laptop could not keep the comment.")
		return nil
	}
	call.reply(nil)
	line := aiOneLine(text)
	if r := []rune(line); len(r) > aiChatCommentToastRunes {
		line = string(r[:aiChatCommentToastRunes]) + "…"
	}
	where := "this chat"
	if a.ID != s.conv.ID {
		where = "“" + aiOneLine(title) + "”"
	}
	return m.pushToastDetail(toast.Info, who+" commented on "+where, line+"\nSee it in AI: Chat Comments…")
}

// openAIShareChat asks first what the link shares.
func (m *Model) openAIShareChat() tea.Cmd {
	if m.confirmOpen {
		return m.aiInfo("Answer the open card first")
	}
	title := aiOneLine(m.aiS().conv.Title)
	if title == "" {
		title = "this chat"
	}
	body := "termocode makes a viewer link (as Web: Share as Viewer) for one person, with \"" + title + "\" open.\n" +
		"They watch the web panel read only: this chat, live, and the rest of the panel too.\n" +
		"They cannot type into the chat. They can leave comments; you read them in AI: Chat Comments….\n" +
		"The AI never reads a comment unless you add it to your message."
	m.confirm = confirm.New("Share this chat", body, []confirm.Button{
		{ID: "cancel", Title: "Cancel"},
		{ID: "share", Title: "Make link"},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIShareChat
	return nil
}

// answerAIShareChat makes the link on Make link; anything else is Cancel.
func (m *Model) answerAIShareChat(id string) tea.Cmd {
	m.confirmOpen = false
	if id != "share" {
		return nil
	}
	m.aiS().agents.on = false // the panel shows the chat being shared
	return tea.Batch(m.openAIPanel(true), m.openWebPair(true))
}

// openAIChatComments lists the open chat's comments, newest first.
func (m *Model) openAIChatComments() tea.Cmd {
	m.aiChatCommentsFor = m.aiS().conv.ID
	m.picker = picker.NewItems(" Comments on this chat ", m.aiChatCommentItems())
	m.picker.SetNote([]string{"Enter adds a comment to your message (you send it)"})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAIChatComments
	return nil
}

// aiChatCommentItems are the rows of the comments picker.
func (m *Model) aiChatCommentItems() []picker.Item {
	list := aiChatCommentsLoad(m.aiChatCommentsFor)
	if len(list) == 0 {
		return []picker.Item{{Header: true, Title: "No comments on this chat yet. Share it with AI: Share Chat by Link…", Group: "x"}}
	}
	out := make([]picker.Item, 0, len(list))
	for i := len(list) - 1; i >= 0; i-- {
		c := list[i]
		out = append(out, picker.Item{ID: aiChatCommentPfx + strconv.Itoa(i), Title: aiOneLine(c.Text), Hint: c.Who + " · " + aiAge(c.AtMs)})
	}
	return out
}

// onAIChatCommentPicked asks before the comment goes into the input.
func (m *Model) onAIChatCommentPicked(id string) tea.Cmd {
	n, err := strconv.Atoi(strings.TrimPrefix(id, aiChatCommentPfx))
	list := aiChatCommentsLoad(m.aiChatCommentsFor)
	if err != nil || n < 0 || n >= len(list) {
		return nil
	}
	m.aiChatCommentPick = list[n]
	c := list[n]
	m.confirm = confirm.New("Add this comment to your message?",
		c.Who+" wrote:\n\n"+c.Text+"\n\nIt goes into the chat input. Nothing is sent until you press Enter.",
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "add", Title: "Add to message"}}).WithScrollText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIChatComment
	return nil
}

// answerAIChatComment adds the picked comment to the input on Add.
func (m *Model) answerAIChatComment(id string) tea.Cmd {
	m.confirmOpen = false
	c := m.aiChatCommentPick
	m.aiChatCommentPick = aiChatComment{}
	if id != "add" || c.Text == "" {
		return nil
	}
	s := m.aiS()
	if s.conv.ID != m.aiChatCommentsFor {
		return m.aiInfo("Not added: another chat is open now")
	}
	add := "Comment from " + c.Who + ":\n" + c.Text
	if len(s.input) > 0 {
		add = "\n\n" + add
	}
	s.input = append(s.input, []rune(add)...)
	s.caret = len(s.input)
	return tea.Batch(m.openAIPanel(true), m.aiInfo("Comment added to your message: check it, then press Enter"))
}
