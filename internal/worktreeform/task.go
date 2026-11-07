package worktreeform

import (
	"strings"
	"unicode"
)

// Names from the agent task (CMP-26): when the user types only a Task on
// Worktree: New…, the branch and the worktree's name are drafted from
// the task's first line. Both are shown on the card before Create, and a
// typed name or branch always wins.

// MaxTaskWords is how many words of a task go into its branch.
const MaxTaskWords = 5

// MaxTitle is the longest name drafted from a task.
const MaxTitle = 48

// leadFiller are the polite words a task often starts with; they are
// dropped from the start of the name.
var leadFiller = setOf("please", "pls", "kindly", "can", "could", "would", "will", "you",
	"i", "we", "want", "need", "to", "let's", "lets", "let", "us", "me", "help", "just",
	"hey", "hi", "hello", "ok", "okay")

// branchFiller are dropped from anywhere in the branch: they add length,
// not meaning.
var branchFiller = union(leadFiller, setOf("s", "the", "a", "an", "this", "that", "it",
	"some", "our", "my", "your", "of", "for", "in", "on", "at", "with", "from", "by",
	"into", "and"))

// taskKinds gives the kind prefix of a branch from the task's first
// word.
var taskKinds = map[string]string{
	"fix": "fix", "fixes": "fix", "bug": "fix", "repair": "fix",
	"add": "feat", "implement": "feat", "create": "feat", "build": "feat", "support": "feat", "make": "feat",
	"refactor": "refactor", "clean": "refactor", "cleanup": "refactor", "simplify": "refactor", "rename": "refactor", "move": "refactor",
	"docs": "docs", "doc": "docs", "document": "docs",
	"test": "test", "tests": "test",
}

// kindVerb are first words the kind prefix already says: they are not
// repeated after it (fix/login-timeout, not fix/fix-login-timeout).
var kindVerb = setOf("fix", "fixes", "refactor", "docs", "doc", "document", "test", "tests")

func setOf(w ...string) map[string]bool {
	m := make(map[string]bool, len(w))
	for _, s := range w {
		m[s] = true
	}
	return m
}

func union(a, b map[string]bool) map[string]bool {
	m := make(map[string]bool, len(a)+len(b))
	for k := range a {
		m[k] = true
	}
	for k := range b {
		m[k] = true
	}
	return m
}

// firstLine is the task's first line with text, without a leading
// list or heading mark.
func firstLine(task string) string {
	for _, l := range strings.Split(task, "\n") {
		l = strings.TrimLeft(strings.TrimSpace(l), "#*->` \t")
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// BranchFromTask drafts a branch from the first line of an agent task:
// lower case words without filler, at most MaxTaskWords of them. A known
// first verb gives the kind prefix (fix/, feat/, refactor/, docs/,
// test/); a repo prefix (worktrees.json branch_prefix) is used instead
// when set. "" when nothing is left.
func BranchFromTask(task, prefix string) string {
	var words []string
	for _, w := range strings.FieldsFunc(strings.ToLower(firstLine(task)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if !branchFiller[w] {
			words = append(words, w)
		}
	}
	if len(words) == 0 {
		return ""
	}
	pre := BranchFromName(strings.TrimSpace(prefix))
	if pre == "" {
		if k := taskKinds[words[0]]; k != "" && (!kindVerb[words[0]] || len(words) > 1) {
			pre = k
			if kindVerb[words[0]] {
				words = words[1:]
			}
		}
	}
	if len(words) > MaxTaskWords {
		words = words[:MaxTaskWords]
	}
	slug := BranchFromName(strings.Join(words, " "))
	if pre == "" {
		return slug
	}
	return BranchFromName(pre + "/" + slug)
}

// TitleFromTask drafts the worktree's name from the first line of an
// agent task: the polite lead-in and the end mark go, the first letter
// is a capital, cut on a word to at most MaxTitle bytes.
func TitleFromTask(task string) string {
	words := strings.Fields(firstLine(task))
	for len(words) > 0 && leadFiller[strings.ToLower(strings.Trim(words[0], ",.!?:;"))] {
		words = words[1:]
	}
	var b strings.Builder
	for _, w := range words {
		add := len(w)
		if b.Len() > 0 {
			add++
		}
		if b.Len()+add > MaxTitle {
			break
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(w)
	}
	out := strings.TrimRight(b.String(), ".!?,:; ")
	if out == "" {
		return ""
	}
	r := []rune(out)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
