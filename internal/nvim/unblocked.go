package nvim

import (
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
)

// ErrBlocked is returned by a call made while nvim waits for the rest of a
// key sequence (the character after f / t / r / q / m, a digraph after
// Ctrl+K, an input() prompt, …). The call still runs once nvim gets that
// input; its result is dropped.
var ErrBlocked = errors.New("nvim: waiting for input")

// blockedPoll is how often a pending call checks whether nvim blocks.
const blockedPoll = 10 * time.Millisecond

// unblocked runs f, a synchronous RPC call, but gives up with ErrBlocked
// when nvim is blocked waiting for input. While blocked, nvim answers only
// "fast" calls (nvim_get_mode, nvim_input); any other call waits for the
// user's next key. The app makes such calls from Bubble Tea's Update, so
// one key like "f" in Normal mode froze the whole UI: Update waited for
// nvim, nvim waited for a key, and keys only reach nvim through Update.
// Hit-enter prompts ("r…" modes) are not counted: promptGuard answers
// them within ~100 ms and the call then completes.
func (c *Client) unblocked(f func() error) error {
	done := make(chan error, 1)
	crash.GoWith("an editor call", func() { done <- f() }, func() {
		select {
		case done <- errors.New("the editor call failed"):
		default: // f had answered
		}
	})
	tick := time.NewTicker(blockedPoll)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-tick.C:
			if c.nv == nil { // not connected (a test client): nothing can block
				continue
			}
			m, err := c.nv.Mode()
			if err == nil && m.Blocking && !strings.HasPrefix(m.Mode, "r") {
				return ErrBlocked
			}
		}
	}
}

// execLua is nvim_exec_lua through unblocked. The result is decoded into
// a fresh value and copied to result only on success, so a call that
// finishes after ErrBlocked never writes into the caller's variable.
func (c *Client) execLua(code string, result any, args ...any) error {
	target := result
	var tmp reflect.Value
	if rv := reflect.ValueOf(result); result != nil && rv.Kind() == reflect.Pointer && !rv.IsNil() {
		tmp = reflect.New(rv.Type().Elem())
		target = tmp.Interface()
	}
	err := c.unblocked(func() error { return c.nv.ExecLua(code, target, args...) })
	if err == nil && tmp.IsValid() {
		reflect.ValueOf(result).Elem().Set(tmp.Elem())
	}
	return err
}
