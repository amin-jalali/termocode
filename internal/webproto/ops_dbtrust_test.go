package webproto

import "testing"

// Spec: db.trust_host saves the SSH host key the page was asked about
// (named by its fingerprint) to known_hosts on the laptop: a controller
// only, slow, audited; nothing but the connection and the fingerprint.
func TestDBTrustHostOp(t *testing.T) {
	const op = "db.trust_host"
	checkArgs(t, op,
		[]string{`{"conn":"tun"}`, `{"conn":"tun","fingerprint":""}`, `{"conn":"tun","fingerprint":"MD5:aa:bb"}`, `{"conn":"","fingerprint":"SHA256:abc"}`,
			`{"conn":"tun","fingerprint":"SHA256:a b"}`, `{"conn":"tun","fingerprint":"SHA256:"}`},
		[]string{`{"conn":"tun","fingerprint":"SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8"}`})
	checkRow(t, op, RoleController, false, AuditYes)
	if p, _ := Lookup(op); p.Rate != slow {
		t.Errorf("rate %+v, want slow", p.Rate)
	}
}
