package webproto

import (
	"encoding/json"
	"errors"
	"testing"
)

// Spec (WEB-57): images are v2 controller reads with a path or a chat
// message and image index; bad args are refused.
func TestImageOps(t *testing.T) {
	for _, c := range []struct{ op, args string }{
		{OpFileImage, `{"path":"../x.png"}`},
		{OpFileImage, `{"path":"/etc/x.png"}`},
		{OpChatImage, `{"i":-1,"n":0}`},
		{OpChatImage, `{"i":0,"n":-1}`},
	} {
		if _, err := DecodeArgs(c.op, json.RawMessage(c.args)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s %s: %v", c.op, c.args, err)
		}
	}
	for _, op := range []string{OpFileImage, OpChatImage} {
		p, _ := Lookup(op)
		if p.Role != RoleController || p.Phase != PhaseV2 || p.LaptopCard {
			t.Errorf("%s row %+v", op, p)
		}
		if _, ok := ResultOf(op); !ok {
			t.Errorf("%s has no result type", op)
		}
	}
}
