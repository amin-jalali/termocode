package agents

import "regexp"

// CMP-64: opt-in auto-accept. acceptTable is, per agent, the line of a
// permission prompt whose cursor sits on the "yes, this once" choice and
// the keys that pick it. "Always" / "don't ask again" choices are never
// picked, and a question with its own choices (no "Yes" row under the
// cursor) has no match, so it stays with the user. Samples: testdata/.
type acceptRule struct {
	line *regexp.Regexp
	keys string
}

var acceptTable = map[string][]acceptRule{
	"claude":   {{regexp.MustCompile(`^\s*[│|]?\s*❯\s*1\.\s*Yes\s*[│|]?\s*$`), "\r"}},
	"codex":    {{regexp.MustCompile(`(?i)^\s*›\s*1\.\s*yes, proceed\b`), "\r"}},
	"gemini":   {{regexp.MustCompile(`(?i)^\s*[│|]?\s*●\s*1\.\s*yes, allow once\b`), "\r"}},
	"opencode": {{regexp.MustCompile(`(?i)enter accept\s+a accept always`), "\r"}},
	"aider":    {{regexp.MustCompile(`\(Y\)es/\(N\)o`), "y\r"}},
}

// HasAcceptRules says whether AcceptOnce can answer agent id's prompts.
func HasAcceptRules(id string) bool {
	_, ok := acceptTable[id]
	return ok
}

// AcceptOnce gives the keys that answer agent id's permission prompt on
// the bottom of the screen with "yes, this once"; false when no known
// prompt is there.
func AcceptOnce(id string, lines []string) (string, bool) {
	rs, ok := acceptTable[id]
	if !ok {
		return "", false
	}
	for _, l := range bottom(lines) {
		for _, r := range rs {
			if r.line.MatchString(l) {
				return r.keys, true
			}
		}
	}
	return "", false
}

// riskyWords are commands auto-accept never says yes to: they delete,
// rewrite history, run as root, pipe a download into a shell, touch keys
// or remove cluster / cloud objects.
var riskyWords = regexp.MustCompile(`(?i)(\brm\s+-[a-z]*[rf][a-z]*\b|\bsudo\b|\bdoas\b|\bgit\s+push\b.*(--force|\s-f\b|--mirror|--delete)|\bgit\s+reset\s+--hard\b|\bgit\s+clean\s+-[a-z]*f|\bgit\s+branch\s+-D\b|\bgit\s+checkout\s+--\s|\bdrop\s+(table|database|schema)\b|\btruncate\s+table\b|\bmkfs\b|\bdd\s+if=|\bchmod\s+-R\b|\bchown\s+-R\b|\b(curl|wget)\b.*\|\s*(ba|z)?sh\b|:\(\)\s*\{|>\s*/dev/sd|\bshutdown\b|\breboot\b|\bkubectl\s+delete\b|\bhelm\s+uninstall\b|\bterraform\s+destroy\b|\bdocker\s+(system\s+prune|rm\s+-f)|\.ssh/|\bid_rsa\b|\bid_ed25519\b|(^|[\s/])\.env\b|\.aws/credentials)`)

// RiskyPrompt finds a risky command on the bottom lines of the screen
// (the prompt and the command it shows); it returns the matched words.
func RiskyPrompt(lines []string) (string, bool) {
	for _, l := range bottom(lines) {
		if w := riskyWords.FindString(l); w != "" {
			return w, true
		}
	}
	return "", false
}
