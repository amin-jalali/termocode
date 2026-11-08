package webproto

import (
	"strings"
	"testing"
)

// Spec: the hosts tree. containers.servers lists every host and
// containers.server one host's containers: viewers may read them, not
// audited; a host is a context name. containers.add_server is + Add
// Server…: a controller only, slow, audited, no laptop card (the page
// asks first); the name is a context name and the host exactly
// ssh://user@host[:port] (no password, path or shell text).
// db.add_container adds a listed container of a listed host: a
// controller only, slow, audited.
func TestServersOps(t *testing.T) {
	checkRow(t, "containers.servers", RoleViewer, false, AuditNo)
	checkArgs(t, "containers.server", []string{`{"name":""}`, `{"name":"-x"}`, `{"name":"a b"}`, `{"name":"a;b"}`}, []string{`{"name":"baaytona"}`, `{"name":"prod.eu-1"}`})
	checkRow(t, "containers.server", RoleViewer, false, AuditNo)

	const add = "containers.add_server"
	checkArgs(t, add,
		[]string{`{"name":"baaytona"}`, `{"name":"","host":"ssh://deploy@h"}`, `{"name":"-b","host":"ssh://deploy@h"}`, `{"name":"a b","host":"ssh://deploy@h"}`,
			`{"name":"b","host":"deploy@h"}`, `{"name":"b","host":"ssh://h"}`, `{"name":"b","host":"tcp://h:2376"}`, `{"name":"b","host":"ssh://deploy:pw@h"}`,
			`{"name":"b","host":"ssh://deploy@h/x"}`, `{"name":"b","host":"ssh://deploy@h?x=1"}`, `{"name":"b","host":"ssh://deploy@h:0"}`, `{"name":"b","host":"ssh://deploy@h:99999"}`,
			`{"name":"b","host":"ssh://-oProxyCommand=x@h"}`, `{"name":"b","host":"ssh://deploy@-h"}`, `{"name":"b","host":"ssh://deploy@h x"}`,
			`{"name":"b","host":"ssh://deploy@h\n"}`, `{"name":"b","host":"ssh://deploy@` + strings.Repeat("h", 260) + `"}`},
		[]string{`{"name":"baaytona","host":"ssh://deploy@65.109.212.14"}`, `{"name":"b2","host":"ssh://ops@db.example.com:2222"}`})
	checkRow(t, add, RoleController, false, AuditYes)
	if p, _ := Lookup(add); p.Rate != slow {
		t.Errorf("rate %+v, want slow", p.Rate)
	}

	const pick = "db.add_container"
	checkArgs(t, pick, []string{`{"host":"baaytona"}`, `{"host":"","id":"c0ffee01aa00"}`, `{"host":"baaytona","id":"zz"}`, `{"host":"baaytona","id":"c0ffee"}`},
		[]string{`{"host":"baaytona","id":"c0ffee01aa00"}`})
	checkRow(t, pick, RoleController, false, AuditYes)
	if p, _ := Lookup(pick); p.Rate != slow {
		t.Errorf("rate %+v, want slow", p.Rate)
	}
}
