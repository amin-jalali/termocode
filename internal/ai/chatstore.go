package ai

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// Stored chat roles. "tool" and "error" lines are kept for display only;
// they are not replayed to the model.
const (
	StoredUser      = "user"
	StoredAssistant = "assistant"
	StoredTool      = "tool"
	StoredError     = "error"
)

// StoredMessage is one persisted chat line.
type StoredMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// Conversation is one persisted chat. Port of mobocode's chat_store.dart,
// but one JSON file per chat under ai/chats/ so a write never rewrites
// the whole history.
type Conversation struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Messages    []StoredMessage `json:"messages"`
	UpdatedAtMs int64           `json:"updated_at_ms"`
}

const (
	defaultChatTitle = "New chat"
	maxChats         = 50
	titleMaxLen      = 40
)

// ChatStore persists conversations in Dir. Every method is best-effort
// about corrupt files: they are skipped, never returned as errors.
type ChatStore struct{ Dir string }

// DefaultChatStore stores chats in <config>/ai/chats.
func DefaultChatStore() ChatStore {
	d, err := ConfigDir()
	if err != nil {
		return ChatStore{}
	}
	return ChatStore{Dir: filepath.Join(d, "ai", "chats")}
}

var idCounter atomic.Int64

// NewConversation returns an empty, unsaved conversation with a fresh id.
func NewConversation() Conversation {
	now := time.Now().UnixMilli()
	return Conversation{
		ID:          fmt.Sprintf("%d-%d", now, idCounter.Add(1)),
		Title:       defaultChatTitle,
		UpdatedAtMs: now,
	}
}

// DeriveTitle returns the first user line (≤ 40 chars) or "New chat".
func DeriveTitle(msgs []StoredMessage) string {
	for _, m := range msgs {
		if m.Role != StoredUser {
			continue
		}
		line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(m.Text), "\n", 2)[0])
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > titleMaxLen {
			line = strings.TrimRight(string(r[:titleMaxLen]), " ")
		}
		return line
	}
	return defaultChatTitle
}

func (s ChatStore) path(id string) string {
	safe := strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return r
		}
		return '_'
	}, id)
	return filepath.Join(s.Dir, safe+".json")
}

// Save writes c (refreshing its title and timestamp) and prunes the store
// to the newest 50 chats. Empty conversations are not written.
func (s ChatStore) Save(c *Conversation) error {
	if s.Dir == "" || c == nil || c.ID == "" || len(c.Messages) == 0 {
		return nil
	}
	if c.Title == "" || c.Title == defaultChatTitle {
		c.Title = DeriveTitle(c.Messages)
	}
	c.UpdatedAtMs = time.Now().UnixMilli()
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(c.ID) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path(c.ID)); err != nil {
		return err
	}
	s.prune()
	return nil
}

// Load reads one conversation.
func (s ChatStore) Load(id string) (Conversation, error) {
	var c Conversation
	if s.Dir == "" || id == "" {
		return c, os.ErrNotExist
	}
	b, err := os.ReadFile(s.path(id))
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Conversation{}, err
	}
	return c, nil
}

// List returns every stored conversation, newest first.
func (s ChatStore) List() []Conversation {
	if s.Dir == "" {
		return nil
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil
	}
	var out []Conversation
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			continue
		}
		var c Conversation
		if json.Unmarshal(b, &c) != nil || c.ID == "" {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAtMs > out[j].UpdatedAtMs })
	return out
}

// Delete removes one conversation.
func (s ChatStore) Delete(id string) error {
	if s.Dir == "" {
		return nil
	}
	err := os.Remove(s.path(id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s ChatStore) prune() {
	list := s.List()
	for i := maxChats; i < len(list); i++ {
		_ = s.Delete(list[i].ID)
	}
}
