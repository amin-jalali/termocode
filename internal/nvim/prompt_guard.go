package nvim

import (
	"strings"
	"time"
)

// PromptMsg reports that nvim showed a blocking prompt (hit-enter, more,
// confirm) and the client answered it. Text is the last message, if any.
type PromptMsg struct {
	Mode string
	Text string
}

// promptGuardInterval is how often the guard asks nvim for its mode.
// nvim_get_mode is a "fast" call: nvim answers it even while it waits in
// a prompt, and it costs almost nothing.
const promptGuardInterval = 100 * time.Millisecond

// promptGuard keeps nvim out of blocking prompts. termocode runs with
// cmdheight=0, so any message from user input (an error such as "E433:
// No tags file" after Ctrl+], a failed :command, a multi-line echo) opens
// a hit-enter prompt. While nvim waits there it answers no other RPC call,
// so the next call from the app's Update loop hangs and the UI freezes
// for good (keys can no longer reach nvim to dismiss it).
//
// The guard polls nvim_get_mode and answers such prompts: <CR> for
// hit-enter, q for -- More --, <Esc> for a confirm dialog. Then it reports
// a PromptMsg so the app can show the message as a toast.
func (c *Client) promptGuard() {
	t := time.NewTicker(promptGuardInterval)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-c.exited:
			return
		case <-t.C:
		}
		m, err := c.nv.Mode()
		if err != nil || !m.Blocking || !strings.HasPrefix(m.Mode, "r") {
			continue
		}
		key := "<CR>"
		switch m.Mode {
		case "rm":
			key = "q"
		case "r?":
			key = "<Esc>"
		}
		if _, err := c.nv.Input(key); err != nil {
			continue
		}
		msg := PromptMsg{Mode: m.Mode}
		select {
		case c.events <- msg:
		default: // never block on a full queue
		}
	}
}
