package assets

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// "↩ to chat" (MOBUX-19), checked in the source until the phone e2e runs
// it: every row and block can put a reference into the composer. A
// reference is kind / id / path / line only, never the thing's contents;
// a secret-looking file is refused on the phone ("stays on the laptop");
// at most 8 go with one message; the button is a 44 px target with a
// screen reader label and a pressed state.
func TestToChatSendsOnlyAReference(t *testing.T) {
	src := toChatSrc(t, "src/lib/tochat.ts")
	for _, want := range []string{
		`export const MAX_REFS = 8;`,
		`export function addRef(`,
		`export function removeRef(`,
		`export function clearRefs(`,
		`export function attachArgs(`,
		`export function blockRef(`,
		`export function itemRef(`,
		`export function rowRef(`,
		`export function secretPath(`,
		`looksSecret(`, // a secret-looking title never becomes a chip label
	} {
		if !strings.Contains(src, want) {
			t.Errorf("tochat.ts lost %q", want)
		}
	}
	// cleanRef is the one place a ref is copied: it keeps only the four
	// reference fields, so no label, text or content can ride along.
	m := regexp.MustCompile(`(?s)export function cleanRef\(.*?\n}\n`).FindString(src)
	if m == "" {
		t.Fatal("tochat.ts has no cleanRef")
	}
	for _, bad := range []string{"text", "content", "body", "label", "value", "...r", "...ref"} {
		if regexp.MustCompile(`\b`+regexp.QuoteMeta(bad)+`\s*:`).MatchString(m) || strings.Contains(m, "{ ...") {
			t.Errorf("cleanRef copies %q: a reference must stay kind / id / path / line", bad)
		}
	}
	if !regexp.MustCompile(`(?s)export function attachArgs\(.*?cleanRef`).MatchString(src) {
		t.Error("attachArgs does not send the refs through cleanRef")
	}
	for _, p := range []string{`\.env`, `id_`, `pem`, `\.ssh`, `credentials`} {
		if !strings.Contains(src, p) {
			t.Errorf("secretPath lost the %q rule", p)
		}
	}
	if !strings.Contains(src, `startsWith("/")`) || !strings.Contains(src, `".."`) {
		t.Error("cleanRef must refuse absolute paths and .. parts")
	}
}

func TestToChatButtonIsATarget(t *testing.T) {
	btn := toChatSrc(t, "src/ui/ToChat.tsx")
	for _, want := range []string{`class={`, `tochat`, `aria-label={t("tochat.add"`, `aria-pressed=`, `addRef(`, `removeRef(`, `"tochat.secret"`, `"tochat.full"`} {
		if !strings.Contains(btn, want) {
			t.Errorf("ToChat.tsx lost %q", want)
		}
	}
	chips := toChatSrc(t, "src/ui/RefChips.tsx")
	for _, want := range []string{`composerRefs.value`, `removeRef(`, `aria-label={t("tochat.remove"`} {
		if !strings.Contains(chips, want) {
			t.Errorf("RefChips.tsx lost %q", want)
		}
	}
	css := toChatSrc(t, "src/styles/chat.css")
	rule := regexp.MustCompile(`\.tochat \{[^}]*\}`).FindString(css)
	if !strings.Contains(rule, "min-width: var(--tap)") || !strings.Contains(rule, "min-height: var(--tap)") {
		t.Errorf("the ↩ button is not a 44 px target: %q", rule)
	}
	chip := regexp.MustCompile(`\.ref-chip \{[^}]*\}`).FindString(css)
	if !strings.Contains(chip, "min-height: var(--tap)") {
		t.Errorf("a composer ref chip is not a 44 px target: %q", chip)
	}
}

func TestComposerSendsTheRefs(t *testing.T) {
	src := toChatSrc(t, "src/views/Composer.tsx")
	for _, want := range []string{`attachArgs()`, `clearRefs()`, `<RefChips />`} {
		if !strings.Contains(src, want) {
			t.Errorf("Composer.tsx lost %q", want)
		}
	}
	// The refs leave only with a send that the laptop took.
	if strings.Index(src, `clearRefs()`) < strings.Index(src, `link.request("chat.send"`) {
		t.Error("the refs are cleared before the laptop took the message")
	}
}

func toChatSrc(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Every item, table row and block head of the phone's blocks carries ↩,
// and an attach action goes to the composer on the page; the strings are
// the phone's English-only ones (strings.phone.ts), not app.js's.
func TestEveryBlockRowHasToChat(t *testing.T) {
	src := toChatSrc(t, "src/ui/Blocks.tsx")
	for _, want := range []string{`<ToChat to={itemRef(block, it)}`, `<ToChat to={rowRef(block, base + i)}`, `<ToChat to={blockRef(b)}`, `a.kind === "attach" ? attachAction(`} {
		if !strings.Contains(src, want) {
			t.Errorf("Blocks.tsx lost %q", want)
		}
	}
	if strings.Contains(toChatSrc(t, "src/strings.ts"), `"tochat.`) || strings.Contains(toChatSrc(t, "src/strings.fa.ts"), `"tochat.`) {
		t.Error("the ↩ strings belong in strings.phone.ts (English only)")
	}
	if !strings.Contains(toChatSrc(t, "src/strings.phone.ts"), `"tochat.add"`) {
		t.Error("strings.phone.ts has no tochat.add")
	}
}
