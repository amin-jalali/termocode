package app

import (
	"errors"

	"github.com/amin-jalali/termocode/internal/ai"
)

// holdChat makes this window the holder of the stored chat id (SCN-10:
// ai.ChatStore.Claim) and lets the one it held before go. false: another
// termocode window holds it, so this one must not show and save it. A
// claim that cannot be checked at all (no lock file possible) does not
// block the chat.
func (s *aiState) holdChat(id string) bool {
	if s.claim != nil && s.claim.ID() == id {
		return true
	}
	c, err := s.store.Claim(id)
	if err != nil {
		return !errors.Is(err, ai.ErrChatInUse)
	}
	s.claim.Release()
	s.claim = c
	return true
}
