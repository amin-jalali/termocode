//go:build unix

package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// The phone's Term page (long press → Ask AI about this / Add to chat):
// a term attach gives the model the shell's rows the user picked, read on
// the laptop under the rules of every attach; a shell the laptop does not have is not_found.
func TestChatSendAttachTermLines(t *testing.T) {
	m := webTestChatModel(t)
	webTestFakeAI(&m, "ok")
	webTestHub(t, &m)
	m.terminalTabs = []terminalTab{{BufID: 3, Name: "zsh"}}
	old := webTermLines
	var asked [3]int
	webTermLines = func(_ *nvim.Client, buf, from, to int) ([]string, error) {
		asked = [3]int{buf, from, to}
		return []string{"--- FAIL: TestDiscount (0.00s)", "    cart_test.go:24: Total = 10, want 9", "FAIL"}, nil
	}
	t.Cleanup(func() { webTermLines = old })
	c := webTestSub(t, &m, "chat")
	f := webTestSendAttached(t, &m, c, `{"text":"Explain these lines","attach":[{"kind":"term","id":"3","from":2,"to":4,"label":"3 lines · terminal zsh"}]}`)
	if f["t"] != "res" {
		t.Fatalf("send: %v", f)
	}
	if asked != [3]int{3, 2, 4} {
		t.Errorf("read rows %v, want buffer 3 rows 2..4", asked)
	}
	um := m.ai.conv.Messages[0]
	if !strings.Contains(um.Context, "want 9") || !strings.Contains(um.Context, "Terminal lines 2–4") {
		t.Fatalf("context %q", um.Context)
	}
	if len(um.Attached) != 1 || um.Attached[0].Kind != "term" || um.Attached[0].Label != "3 lines · terminal zsh" {
		t.Fatalf("chips %+v", um.Attached)
	}
	m.ai.streaming = false
	if f := webTestSendAttached(t, &m, c, `{"text":"and this?","attach":[{"kind":"term","id":"9","from":1,"to":1}]}`); webTestErr(f) != "not_found" {
		t.Fatalf("unknown shell: %v", f)
	}
}
