package webproto

import (
	"strings"
	"testing"
)

// Spec: db.secret hands the laptop a database password, an SSH password
// or an SSH key passphrase the page was asked for, once, to keep in its
// secret store: a controller only, slow, audited (never the value); the
// kind is one of the three and the value is one line of text.
func TestDBSecretOp(t *testing.T) {
	const op = "db.secret"
	checkArgs(t, op,
		[]string{`{"conn":"tun","kind":"ssh_password"}`, `{"conn":"tun","kind":"ssh_password","value":""}`, `{"conn":"tun","kind":"token","value":"x"}`,
			`{"conn":"","kind":"password","value":"x"}`, `{"conn":"tun","kind":"password","value":"a\u0000b"}`, `{"conn":"tun","kind":"password","value":"a\nb"}`,
			`{"conn":"tun","kind":"password","value":"` + strings.Repeat("x", 1025) + `"}`},
		[]string{`{"conn":"tun","kind":"password","value":"p ä !#"}`, `{"conn":"tun","kind":"ssh_password","value":"x"}`, `{"conn":"tun","kind":"ssh_passphrase","value":"x"}`})
	checkRow(t, op, RoleController, false, AuditYes)
	if p, _ := Lookup(op); p.Rate != slow {
		t.Errorf("rate %+v, want slow", p.Rate)
	}
}
