package app

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// A database op from the page that fails says why: the server's own
// reason in one line, the password hidden, no host or user, as a
// conflict. "internal" stays for termocode's own faults, which the page
// shows as such.

// webDBMaxReason caps the reason (runes).
const webDBMaxReason = 300

// pgx: "failed to connect to `user=app database=app`: " before the reason.
var webDBConnectTo = lazyre.New("^failed to connect to `[^`]*`: ")

// webDBReason is the one line the page shows for err.
func webDBReason(err error, password string) string {
	text := dbErrorText(err) // the FATAL / ERROR line, or the last reason
	text = webDBConnectTo.ReplaceAllString(text, "")
	text = dbConnectTarget.ReplaceAllString(text, "")
	if mt := dbDialReason.FindStringSubmatch(text); mt != nil {
		text = mt[1]
	}
	text = strings.TrimSpace(dbOneLine(dbAIClean(text, password)))
	if r := []rune(text); len(r) > webDBMaxReason {
		text = string(r[:webDBMaxReason-1]) + "…"
	}
	if text == "" {
		text = "The database did not answer."
	}
	return text
}

// fail answers call with why err happened on w's connection, and what
// the page can ask the user for (a DBNeed) when there is something.
func (w *webDBConn) fail(call *webCall, err error) {
	if need, msg := w.needOf(err); need != nil {
		webFailData(call, webproto.CodeConflict, msg, need)
		return
	}
	if p := dbConnectProblem(w.c, err); p != "" { // the step that failed, with its configured values
		call.fail(webproto.CodeConflict, strings.TrimSpace(dbOneLine(dbAIClean(p, w.snap.password))))
		return
	}
	call.fail(webproto.CodeConflict, webDBReason(err, w.snap.password))
}
