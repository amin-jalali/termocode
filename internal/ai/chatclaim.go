package ai

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Chat claims (SCN-10). Two termocode windows on one folder share the
// chat store and session.json, so the second window restored the first
// one's open chat: both then saved the same file, and the last save
// dropped the other window's turns. The window that shows a stored chat
// holds a claim on it (an flock on <Dir>/.locks/<id>.lock, gone when the
// process ends, also on a crash); another window cannot claim it and
// starts its own chat instead.

// ErrChatInUse: another termocode holds the chat.
var ErrChatInUse = errors.New("this chat is open in another termocode window")

// ChatClaim is this process's hold on one stored chat.
type ChatClaim struct {
	id   string
	path string
	f    *os.File
}

// ID is the claimed chat.
func (c *ChatClaim) ID() string {
	if c == nil {
		return ""
	}
	return c.id
}

var (
	claimsMu sync.Mutex
	claims   = map[string]*ChatClaim{} // lock path → this process's claim
)

// Claim takes the chat for this process. A chat this process holds
// already is fine (the same claim comes back); one another process holds
// is ErrChatInUse. Other errors (no store, a lock that cannot be made)
// mean nothing could be checked.
func (s ChatStore) Claim(id string) (*ChatClaim, error) {
	if s.Dir == "" || id == "" {
		return nil, os.ErrNotExist
	}
	path := filepath.Join(s.Dir, ".locks", filepath.Base(s.path(id))+".lock")
	claimsMu.Lock()
	defer claimsMu.Unlock()
	if c := claims[path]; c != nil {
		return c, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := lockChat(path)
	if err != nil {
		return nil, err
	}
	c := &ChatClaim{id: id, path: path, f: f}
	claims[path] = c
	return c, nil
}

// Release lets the chat go (nil: nothing).
func (c *ChatClaim) Release() {
	if c == nil {
		return
	}
	claimsMu.Lock()
	defer claimsMu.Unlock()
	if claims[c.path] == c {
		delete(claims, c.path)
	}
	if c.f != nil {
		_ = c.f.Close()
		c.f = nil
	}
}
