package assets

import (
	"regexp"
	"strings"
	"testing"
)

// Spec (owner 2026-11-08): a paired page approves its own actions, so no
// text the page shows in that mode says the laptop asks or that the
// answer is on the laptop. Only texts that stay true are kept: the
// laptop-mode question and its waiting state (web_approve_on_laptop on),
// a viewer's line, AI cards that still need the laptop, and a password
// typed only on the laptop.
func TestNoTextSaysTheLaptopAsks(t *testing.T) {
	laptopOnly := map[string]bool{
		"git.ask_laptop":           true, // the laptop-mode question's button
		"wait.e14":                 true, // the laptop-mode question's note
		"card.approve_laptop":      true, // an AI card that needs the laptop
		"card.destructive":         true,
		"card.viewonly":            true, // a viewer
		"nt.viewer_body":           true,
		"ctr.open_db_q":            true, // the password is typed on the laptop only
		"pair.body_approved":       true,
		"pill.waiting_laptop":      true,
		"answered.laptop":          true,
		"wait.title":               true,
		"wait.body":                true,
		"wait.done":                true,
		"wait.cancelled":           true,
		"card.answer_laptop":       true,
		"card.trust.title":         true,
		"start.waiting":            true,
		"rv.discard_laptop":        true,
		"card.long":                true,
		"pair.body_approved_plain": true,
		"pr.ci_loading":            true, // a read, not a question
	}
	en := regexp.MustCompile(`(?i)laptop asks|asks the laptop|ask the laptop|asks on the laptop|answer (it )?(on the laptop|there)|asks you there|laptop card|approve (it )?on (the )?laptop|waits for (a confirmation on )?the laptop`)
	fa := regexp.MustCompile(`لپ‌تاپ (اول |از شما |پیش از [^ ]+ )?می‌پرسد|از لپ‌تاپ پرسیده|از لپ‌تاپ بخواهیم|همان‌?جا می‌پرسد|آنجا پاسخ دهید|کارت لپ‌تاپ`)
	check := func(file, name string, re *regexp.Regexp) {
		for _, e := range parseStrings(t, file, name) {
			if !laptopOnly[e.key] && re.MatchString(e.text) {
				t.Errorf("%s %s: %q says the laptop asks", strings.TrimPrefix(file, "src/"), e.key, e.text)
			}
		}
	}
	check("src/strings.ts", "S", en)
	check("src/strings.desk.ts", "SD", en)
	check("src/strings.phone.ts", "SP", en)
	check("src/strings.fa.ts", "FA", fa)
}
