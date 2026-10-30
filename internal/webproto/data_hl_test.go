package webproto

import (
	"encoding/json"
	"errors"
	"testing"
)

// Spec (WEB-38): buf.hl asks the syntax colours of at most MaxHlRows rows
// of one buffer; an empty range is fine, a reversed or negative one is not.
func TestHlArgsRules(t *testing.T) {
	for _, args := range []string{
		`{"buf":0,"first":0,"last":1}`,
		`{"buf":1,"first":-1,"last":1}`,
		`{"buf":1,"first":5,"last":4}`,
		`{"buf":1,"first":0,"last":2001}`,
	} {
		if _, err := DecodeArgs(OpBufHl, json.RawMessage(args)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", args, err)
		}
	}
	for _, args := range []string{`{"buf":1,"first":0,"last":2000}`, `{"buf":7,"first":40,"last":40}`} {
		if _, err := DecodeArgs(OpBufHl, json.RawMessage(args)); err != nil {
			t.Errorf("%s: %v", args, err)
		}
	}
	p, _ := Lookup(OpBufHl)
	if p.Role != RoleViewer || p.Phase != PhaseV2 || p.Audit != AuditNo {
		t.Errorf("buf.hl row %+v: a v2 read for viewers, not audited", p)
	}
	if _, ok := ResultOf(OpBufHl); !ok {
		t.Error("buf.hl has no result type")
	}
}
