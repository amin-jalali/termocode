package mcptest

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// FakeGH puts an executable "gh" first on PATH that answers like the
// real GitHub CLI signed in with accounts (active is the active one):
//
//	gh auth status                             the real text format
//	gh auth token [--hostname H] [--user X]    prints GHToken(X) (active by default)
//
// With GH_FAKE_NOT_LOGGED_IN=1 both exit 1 like the real gh without a
// login; GH_FAKE_SLEEP=N sleeps N seconds first (timeouts). It never
// talks to GitHub. Unix only (a shell script); the test is skipped
// elsewhere. It returns the script's path.
func FakeGH(t testing.TB, accounts []string, active string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake gh is a shell script")
	}
	dir := t.TempDir()
	var status strings.Builder
	status.WriteString("github.com\n")
	for i, a := range accounts {
		if i > 0 {
			status.WriteString("\n")
		}
		fmt.Fprintf(&status, "  ✓ Logged in to github.com account %s (keyring)\n", a)
		fmt.Fprintf(&status, "  - Active account: %v\n", a == active)
		status.WriteString("  - Git operations protocol: https\n")
		status.WriteString("  - Token: gho_************************************\n")
		status.WriteString("  - Token scopes: 'gist', 'read:org', 'repo', 'workflow'\n")
	}
	var cases strings.Builder
	for _, a := range accounts {
		fmt.Fprintf(&cases, "    %s) echo %s ;;\n", shQuote(a), GHToken(a))
	}
	script := `#!/bin/sh
[ -n "$GH_FAKE_SLEEP" ] && sleep "$GH_FAKE_SLEEP"
if [ "$GH_FAKE_NOT_LOGGED_IN" = "1" ]; then
  echo "You are not logged into any GitHub hosts. To log in, run: gh auth login" >&2
  exit 1
fi
[ "$1" = "auth" ] || { echo "fake gh: unknown command $1" >&2; exit 2; }
case "$2" in
status)
  cat <<'STATUS'
` + status.String() + `STATUS
  ;;
token)
  user=` + shQuote(active) + `
  shift 2
  while [ $# -gt 0 ]; do
    case "$1" in
    --user|-u) user="$2"; shift 2 ;;
    --hostname|-h) shift 2 ;;
    *) shift ;;
    esac
  done
  case "$user" in
` + cases.String() + `    *) echo "no oauth token found for github.com account $user" >&2; exit 1 ;;
  esac
  ;;
*) echo "fake gh: unknown auth command $2" >&2; exit 2 ;;
esac
`
	p := filepath.Join(dir, "gh")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return p
}

// GHToken is the token the fake gh prints for an account.
func GHToken(account string) string { return "gho_fake_" + account }

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
