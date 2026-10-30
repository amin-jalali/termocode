package webproto

import (
	"errors"
	"testing"
)

func TestEncodeChecksType(t *testing.T) {
	b, err := Encode(&Pong{T: KindPong, N: "7"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"t":"pong","n":"7"}` {
		t.Fatalf("got %s", b)
	}
	if _, err := Encode(&Pong{N: "7"}); err == nil {
		t.Fatal("a message with an empty t was encoded")
	}
	if _, err := Encode(&Pong{T: KindPing, N: "7"}); err == nil {
		t.Fatal("a pong was encoded as a ping")
	}
}

func TestServerDecoderIgnoresUnknownField(t *testing.T) {
	m, err := DecodeServer([]byte(`{"t":"resync","topic":"git","later":{"x":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	r, ok := m.(*Resync)
	if !ok || r.Topic != "git" {
		t.Fatalf("got %#v", m)
	}
	if _, err := DecodeServer([]byte(`{"t":"nope"}`)); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("unknown kind: err = %v", err)
	}
	if _, err := DecodeServer([]byte(`{"t":"hello","v":2,"token":"x"}`)); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("a browser kind decoded as a server message: err = %v", err)
	}
	b, err := DecodeServer([]byte(`{"t":"bye","code":4009}`))
	if err != nil || b.(*ServerBye).Code != CloseStopped {
		t.Fatalf("server bye: %#v %v", b, err)
	}
}
