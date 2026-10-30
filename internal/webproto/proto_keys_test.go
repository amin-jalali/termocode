package webproto

import (
	"strings"
	"testing"
)

// Spec (T49, Go side): a browser message with a prototype key
// ("__proto__", "constructor", "prototype") anywhere, at the top, in
// args or nested deeper, is refused by the strict decoder, as are
// duplicate and case-folded keys.
func TestSchemaRejectsProtoKeys(t *testing.T) {
	for _, k := range []string{"__proto__", "constructor", "prototype"} {
		for _, line := range []string{
			`{"t":"ping","n":"a","` + k + `":{}}`,
			`{"t":"sub","topics":["status"],"` + k + `":1}`,
			`{"t":"req","id":1,"op":"tree.list","args":{"path":"","` + k + `":{"x":1}}}`,
			`{"t":"req","id":1,"op":"buf.set_text","args":{"buf":1,"tick":1,"start":{"row":0,"col":0,"` + k + `":1},"end":{"row":0,"col":0},"lines":[]}}`,
			`{"t":"hello","v":2,"token":"x","resume":{"conn":"c","topics":{"status":1},"` + k + `":1}}`,
		} {
			if _, err := DecodeClient([]byte(line)); err == nil {
				if r, ok := mustReq(line); ok {
					if _, err := DecodeArgs(r.Op, r.Args); err == nil {
						t.Errorf("accepted %s", line)
					}
					continue
				}
				t.Errorf("accepted %s", line)
			}
		}
	}
	for _, line := range []string{
		`{"t":"ping","n":"a","n":"b"}`,
		`{"t":"ping","N":"a"}`,
		`{"t":"req","id":1,"op":"tree.list","args":{"path":"","path":"x"}}`,
	} {
		_, err := DecodeClient([]byte(line))
		if err == nil && strings.Contains(line, `"args"`) {
			r, _ := mustReq(line)
			_, err = DecodeArgs(r.Op, r.Args)
		}
		if err == nil {
			t.Errorf("accepted %s", line)
		}
	}
}

func mustReq(line string) (*Req, bool) {
	m, err := DecodeClient([]byte(line))
	if err != nil {
		return nil, false
	}
	r, ok := m.(*Req)
	return r, ok
}
