// Package chatbridge lets a person ask termocode's AI from a chat app
// (OMX-42): Telegram and Discord bots, spoken to over their HTTP APIs
// with net/http only. A Bridge polls one Connector, answers only the
// people on its allow list, and sends each answer back to the chat the
// question came from. What runs for a question is the caller's Handle.
package chatbridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Message is one text a person wrote to the bot.
type Message struct {
	Chat     string // where to answer (Telegram chat id, Discord channel id)
	User     string // the sender's id (what the allow list holds)
	UserName string
	Text     string
}

// Connector is one chat service.
type Connector interface {
	Name() string
	// Start checks the token and skips what was written while the
	// bridge was off (those questions are not run later).
	Start(ctx context.Context) error
	// Poll waits for new messages (Telegram waits on the server; Discord
	// answers at once and the bridge pauses between polls).
	Poll(ctx context.Context) ([]Message, error)
	Send(ctx context.Context, chat, text string) error
}

// APIError is a failed call: the HTTP status and the service's own
// message. It never holds the token.
type APIError struct {
	Status     int
	Msg        string
	RetryAfter time.Duration // set on 429
}

func (e *APIError) Error() string {
	if e.Msg == "" {
		return fmt.Sprintf("HTTP %d", e.Status)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Msg)
}

// IsUnauthorized reports a token the service does not take (wrong or
// revoked): retrying does not help.
func IsUnauthorized(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusUnauthorized
}

// Bridge answers the allowed people on one connector.
type Bridge struct {
	Conn Connector
	// Allow is the user ids that may ask. Empty: the bridge does not
	// start (a bot is public: anyone can find and write to it).
	Allow []string
	// Handle answers one question (one AI turn).
	Handle func(ctx context.Context, m Message) (string, error)
	// Redact hides secrets in what is sent out (nil: as is).
	Redact func(string) string
	// Pause is the wait between polls (Discord has no long poll).
	Pause time.Duration
	// Log reports what happens, one line each (nil: quiet).
	Log func(string)
	// Sleep waits d or until ctx ends (nil: a timer). A test seam.
	Sleep func(ctx context.Context, d time.Duration)

	told map[string]bool // strangers already told they are not allowed
}

// HelpText is the answer to /start and /help.
const HelpText = "Ask me anything about the project: I answer with termocode's AI.\n" +
	"I only read the files, I never change them. Each message is its own question.\n" +
	"/whoami shows your user id."

const maxBackoff = time.Minute

// Run polls until ctx ends (nil) or the token stops working.
func (b *Bridge) Run(ctx context.Context) error {
	if len(b.Allow) == 0 {
		return errors.New("nobody is allowed: add your user id with --allow (write /whoami to the bot to see it)")
	}
	if b.Handle == nil || b.Conn == nil {
		return errors.New("chat bridge: nothing to answer with")
	}
	if err := b.Conn.Start(ctx); err != nil {
		return fmt.Errorf("%s: %w", b.Conn.Name(), err)
	}
	b.logf("%s: listening; answering %s", b.Conn.Name(), strings.Join(b.Allow, ", "))
	fails := 0
	for ctx.Err() == nil {
		msgs, err := b.Conn.Poll(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			if IsUnauthorized(err) {
				return fmt.Errorf("%s: the bot token is not accepted any more: %w", b.Conn.Name(), err)
			}
			fails++
			wait := min(time.Second<<min(fails-1, 6), maxBackoff)
			var ae *APIError
			if errors.As(err, &ae) && ae.RetryAfter > 0 {
				wait = max(wait, ae.RetryAfter)
			}
			b.logf("%s: %v (again in %s)", b.Conn.Name(), err, wait)
			b.sleep(ctx, wait)
			continue
		}
		fails = 0
		for _, m := range msgs {
			if ctx.Err() != nil {
				return nil
			}
			b.answer(ctx, m)
		}
		if b.Pause > 0 {
			b.sleep(ctx, b.Pause)
		}
	}
	return nil
}

func (b *Bridge) answer(ctx context.Context, m Message) {
	text := strings.TrimSpace(m.Text)
	cmd, _, _ := strings.Cut(strings.Fields(text + " ")[0], "@") // "/start@tc_bot"
	if cmd == "/whoami" {
		b.send(ctx, m.Chat, "Your user id is "+m.User+".")
		return
	}
	if !slices.Contains(b.Allow, m.User) {
		if b.told == nil {
			b.told = map[string]bool{}
		}
		b.logf("%s: not allowed: user %s (%s)", b.Conn.Name(), m.User, m.UserName)
		if !b.told[m.User] {
			b.told[m.User] = true
			b.send(ctx, m.Chat, "This bot only answers its owner. Your user id is "+m.User+".")
		}
		return
	}
	if cmd == "/start" || cmd == "/help" {
		b.send(ctx, m.Chat, HelpText)
		return
	}
	b.logf("%s: question from %s", b.Conn.Name(), m.User)
	ans, err := b.Handle(ctx, m)
	switch {
	case ctx.Err() != nil:
		return
	case err != nil:
		ans = "The AI turn failed: " + err.Error()
	case strings.TrimSpace(ans) == "":
		ans = "(no answer)"
	}
	b.send(ctx, m.Chat, ans)
}

func (b *Bridge) send(ctx context.Context, chat, text string) {
	if b.Redact != nil {
		text = b.Redact(text)
	}
	if err := b.Conn.Send(ctx, chat, text); err != nil {
		b.logf("%s: send: %v", b.Conn.Name(), err)
	}
}

func (b *Bridge) logf(f string, a ...any) {
	if b.Log != nil {
		b.Log(fmt.Sprintf(f, a...))
	}
}

func (b *Bridge) sleep(ctx context.Context, d time.Duration) {
	if b.Sleep != nil {
		b.Sleep(ctx, d)
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// Split cuts text into parts of at most max runes, at a line break when
// one is in the second half of a part.
func Split(text string, max int) []string {
	var parts []string
	r := []rune(text)
	for len(r) > 0 {
		if len(r) <= max {
			parts = append(parts, string(r))
			break
		}
		cut := max
		for i := max - 1; i >= max/2; i-- {
			if r[i] == '\n' {
				cut = i
				break
			}
		}
		parts = append(parts, string(r[:cut]))
		r = r[cut:]
		if len(r) > 0 && r[0] == '\n' {
			r = r[1:]
		}
	}
	return parts
}
