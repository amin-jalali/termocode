package webproto

import (
	"errors"
	"testing"
)

func TestDecodeGatewayLine(t *testing.T) {
	m, err := DecodeGatewayLine([]byte(`{"t":"open","conn":"c-3f9a","role":"controller","device":"loc-3f9a","name":"Phone"}`))
	if err != nil {
		t.Fatal(err)
	}
	o := m.(*Open)
	if o.Conn != "c-3f9a" || o.Role != RoleController || o.Device != "loc-3f9a" || o.Name != "Phone" {
		t.Fatalf("got %#v", o)
	}
	if m, err := DecodeGatewayLine([]byte(`{"t":"ctl"}`)); err != nil || m.Type() != KindCtl {
		t.Fatalf("ctl: %#v %v", m, err)
	}
	bad := map[string]string{
		"browser kind": `{"t":"hello","v":2,"token":"x"}`,
		"extra field":  `{"t":"ctl","x":1}`,
		"no conn":      `{"t":"open","role":"viewer","device":"d"}`,
		"bad role":     `{"t":"open","conn":"c","role":"admin","device":"d"}`,
		"ctrl in name": `{"t":"open","conn":"c","role":"viewer","device":"d","name":"a\nb"}`,
	}
	for name, in := range bad {
		if _, err := DecodeGatewayLine([]byte(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := DecodeGatewayLine([]byte(`{"t":"open","conn":"c","role":"admin","device":"d"}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad role: err = %v", err)
	}
}
