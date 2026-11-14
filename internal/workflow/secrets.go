package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/amin-jalali/termocode/internal/secrets"
)

// Secret checks (plan §6.2). A workflow file travels with the
// repository, so it must hold no secret and no command:
//   - a literal that looks like a key or token is refused;
//   - ${cmd:} is refused anywhere (it would run on the reader's machine);
//   - ${env:} and ${dotenv:} are refused (a file could send the user's
//     environment to a server);
//   - ${secret:NAME} is allowed only in the values of a step's headers
//     and env and in a message step's via; the adapter resolves it at
//     run time. Anywhere else (prompts, bodies, args, titles) it is
//     refused, so a secret never reaches a model or an outside system's
//     text fields.
//
// Messages name the field, never the value.

// Token-like literals: the prefixes of the mcp.json warning
// (internal/mcp secretLiteralRe), with a minimum length, so words like
// "task-list" do not match. A prefix may follow a digit or "_"
// ("0sk-…", "key_ghp_…"), only not a letter; group 1 is that byte.
var tokenRe = regexp.MustCompile(`(?i)(^|[^a-z])((ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[A-Za-z0-9_-]{16,}|xox[pbas]-[A-Za-z0-9-]{10,}|sk-[A-Za-z0-9_-]{20,}|lin_api_[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16}\b|bearer\s+[A-Za-z0-9._~+/=-]{20,}|-----BEGIN [A-Z ]*PRIVATE KEY-----)`)

var (
	refRe       = regexp.MustCompile(`\$\{\s*([A-Za-z]+)\s*:([^}]*)\}`)
	secretRefRe = regexp.MustCompile(`\$\{\s*secret\s*:`)
	envRefRe    = regexp.MustCompile(`(?i)\$\{\s*(env|dotenv)\s*:`)
)

// hideTokens replaces every token-like literal in s.
func hideTokens(s string) string {
	if !tokenRe.MatchString(s) {
		return s
	}
	return tokenRe.ReplaceAllString(s, "${1}(hidden)")
}

// looksSecret reports whether s holds a literal that looks like a key.
// ${…} references are not literals.
func looksSecret(s string) bool {
	return tokenRe.MatchString(refRe.ReplaceAllString(s, ""))
}

type secretMode int

const (
	secretNo    secretMode = iota // ${secret:} refused here
	secretOK                      // a header / env value or via
	secretNever                   // under args, vars, output, undo: never, even below headers
)

// scanSecrets walks the whole tree. key is the key n is the value of.
func (c *checker) scanSecrets(n *node, where string, mode secretMode) {
	switch n.kind {
	case kString:
		c.checkSecretText(n.off, where, n.str, mode == secretOK)
	case kArray:
		for i, v := range n.vals {
			c.scanSecrets(v, fmt.Sprintf("%s[%d]", where, i), mode)
		}
	case kObject:
		for i, k := range n.keys {
			at := k
			if where != "" {
				at = where + "." + k
			}
			c.checkSecretText(n.koff[i], at, k, false)
			child := mode
			switch {
			case mode == secretNever:
			case k == "args" || k == "vars" || k == "output" || k == "undo":
				child = secretNever
			case k == "headers" || k == "env":
				if n.vals[i].kind == kObject {
					for j, hk := range n.vals[i].keys {
						c.checkSecretText(n.vals[i].koff[j], at+"."+hk, hk, false)
						c.scanSecrets(n.vals[i].vals[j], at+"."+hk, secretOK)
					}
					continue
				}
			case k == "via":
				child = secretOK
			default:
				child = secretNo
			}
			c.scanSecrets(n.vals[i], at, child)
		}
	}
}

func (c *checker) checkSecretText(off int, where, s string, refOK bool) {
	if !strings.Contains(s, "${") && !tokenRe.MatchString(s) {
		return
	}
	switch {
	case secrets.HasCmdRef(s):
		c.errAt(off, where, "${cmd:} is not allowed in a workflow file (it would run a command on the reader's machine); use ${secret:NAME} in headers or env")
		return
	case envRefRe.MatchString(s):
		c.errAt(off, where, "${env:} and ${dotenv:} are not allowed in a workflow file; store the value as a secret and use ${secret:NAME} in headers or env")
		return
	}
	if secretRefRe.MatchString(s) {
		if !refOK {
			c.errAt(off, where, "${secret:} is allowed only in headers, env and via; a secret must never be in a prompt, a body, args or a title")
			return
		}
		for _, m := range refRe.FindAllStringSubmatch(s, -1) {
			if strings.EqualFold(m[1], "secret") && secrets.ValidName(strings.TrimSpace(m[2])) != nil {
				c.errAt(off, where, "${secret:NAME} needs a secret name of letters, digits, _, - and .")
				return
			}
		}
	}
	if looksSecret(s) {
		c.errAt(off, where, "looks like a key or token written in the file; store it as a secret and use ${secret:NAME} in headers or env")
	}
}
