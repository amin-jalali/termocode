package source

import "regexp"

// Patterns of secrets that outside tools may print. The adapters that
// do not hold the secret value themselves (gh, database drivers) scrub
// by shape.
var (
	ghTokenRe  = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})\b`)
	urlUserRe  = regexp.MustCompile(`(://[^/\s:@]*):[^/\s@]+@`)
	passwordRe = regexp.MustCompile(`(?i)\b(password|passwd|pwd|token|secret)(\s*[=:]\s*)\S+`)
	bearerRe   = regexp.MustCompile(`(?i)\b(bearer|token)\s+[A-Za-z0-9._~+/=-]{16,}`)
)

// scrubShapes replaces token-like text, URL passwords and key=value
// secrets with redactedText.
func scrubShapes(s string) string {
	s = ghTokenRe.ReplaceAllString(s, redactedText)
	s = urlUserRe.ReplaceAllString(s, "${1}:"+redactedText+"@")
	s = passwordRe.ReplaceAllString(s, "${1}${2}"+redactedText)
	s = bearerRe.ReplaceAllString(s, "${1} "+redactedText)
	return s
}
