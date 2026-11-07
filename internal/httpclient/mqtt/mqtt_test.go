package mqtt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// broker is the far end of a net.Pipe; the test drives it byte by byte.
type broker struct {
	t *testing.T
	c net.Conn
}

func (b broker) expect(want []byte) {
	b.t.Helper()
	got := make([]byte, len(want))
	_ = b.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(b.c, got); err != nil {
		b.t.Fatalf("broker read: %v (want % x)", err, want)
	}
	if !bytes.Equal(got, want) {
		b.t.Fatalf("broker got % x, want % x", got, want)
	}
}

func (b broker) send(p []byte) {
	b.t.Helper()
	if _, err := b.c.Write(p); err != nil {
		b.t.Fatalf("broker write: %v", err)
	}
}

var connectUP = []byte{
	0x10, 0x14,
	0x00, 0x04, 'M', 'Q', 'T', 'T', 0x04, 0xC2, 0x00, 0x1E,
	0x00, 0x02, 't', 'c',
	0x00, 0x01, 'u',
	0x00, 0x01, 'p',
}

// connect runs the handshake and returns the client and the broker.
func connect(t *testing.T) (*Client, broker) {
	t.Helper()
	cc, bc := net.Pipe()
	b := broker{t, bc}
	t.Cleanup(func() { _ = bc.Close() })
	type res struct {
		c   *Client
		err error
	}
	done := make(chan res, 1)
	go func() {
		c, err := NewClient(context.Background(), cc, Options{ClientID: "tc", Username: "u", Password: "p", KeepAlive: 30 * time.Second, CleanSession: true})
		done <- res{c, err}
	}()
	b.expect(connectUP)
	b.send([]byte{0x20, 0x02, 0x00, 0x00})
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	t.Cleanup(func() { _ = r.c.Close() })
	return r.c, b
}

func TestLengthEncoding(t *testing.T) {
	for _, n := range []int{0, 1, 127, 128, 321, 16383, 16384, 2097151, 268435455} {
		enc := encodeLength(n)
		got, err := readLength(bytes.NewReader(enc))
		if err != nil || got != n {
			t.Errorf("%d: % x → %d, %v", n, enc, got, err)
		}
	}
	if !bytes.Equal(encodeLength(321), []byte{0xC1, 0x02}) {
		t.Errorf("321 = % x", encodeLength(321))
	}
	if _, err := readLength(bytes.NewReader([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x01})); err == nil {
		t.Error("a five byte length was read")
	}
}

func TestConnectSendsTheHandshake(t *testing.T) {
	connect(t)
}

func TestConnectRefusedSaysWhy(t *testing.T) {
	cc, bc := net.Pipe()
	defer bc.Close()
	b := broker{t, bc}
	done := make(chan error, 1)
	go func() {
		_, err := NewClient(context.Background(), cc, Options{ClientID: "tc", Username: "u", Password: "p", KeepAlive: 30 * time.Second, CleanSession: true})
		done <- err
	}()
	b.expect(connectUP)
	b.send([]byte{0x20, 0x02, 0x00, 0x05})
	err := <-done
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "p\"") || strings.Contains(err.Error(), "password=") {
		t.Fatalf("the error shows the password: %v", err)
	}
}

func TestConnectTimesOutWithoutAnAnswer(t *testing.T) {
	cc, bc := net.Pipe()
	defer bc.Close()
	go func() { _, _ = io.Copy(io.Discard, bc) }()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := NewClient(ctx, cc, Options{ClientID: "tc"}); err == nil {
		t.Fatal("no error without a CONNACK")
	}
}

func TestPublishQoS0(t *testing.T) {
	c, b := connect(t)
	errc := make(chan error, 1)
	go func() { errc <- c.Publish("a/b", []byte("hi"), true) }()
	b.expect([]byte{0x31, 0x07, 0x00, 0x03, 'a', '/', 'b', 'h', 'i'})
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
}

func TestSubscribeAndReceive(t *testing.T) {
	c, b := connect(t)
	errc := make(chan error, 1)
	go func() { _, err := c.Subscribe(context.Background(), "a/b"); errc <- err }()
	b.expect([]byte{0x82, 0x08, 0x00, 0x01, 0x00, 0x03, 'a', '/', 'b', 0x00})
	b.send([]byte{0x90, 0x03, 0x00, 0x01, 0x00})
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	b.send([]byte{0x30, 0x07, 0x00, 0x03, 'a', '/', 'b', 'h', 'i'})
	select {
	case m := <-c.Messages():
		if m.Topic != "a/b" || string(m.Payload) != "hi" || m.QoS != 0 {
			t.Fatalf("message = %+v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no message")
	}
}

func TestSubscribeRejected(t *testing.T) {
	c, b := connect(t)
	errc := make(chan error, 1)
	go func() { _, err := c.Subscribe(context.Background(), "a/b"); errc <- err }()
	b.expect([]byte{0x82, 0x08, 0x00, 0x01, 0x00, 0x03, 'a', '/', 'b', 0x00})
	b.send([]byte{0x90, 0x03, 0x00, 0x01, 0x80})
	if err := <-errc; err == nil || !strings.Contains(err.Error(), "refused the subscription") {
		t.Fatalf("err = %v", err)
	}
}

func TestQoS1MessageIsAcked(t *testing.T) {
	c, b := connect(t)
	b.send([]byte{0x32, 0x09, 0x00, 0x03, 'a', '/', 'b', 0x00, 0x07, 'h', 'i'})
	b.expect([]byte{0x40, 0x02, 0x00, 0x07})
	select {
	case m := <-c.Messages():
		if m.Topic != "a/b" || string(m.Payload) != "hi" || m.QoS != 1 {
			t.Fatalf("message = %+v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no message")
	}
}

func TestPingAndClose(t *testing.T) {
	c, b := connect(t)
	errc := make(chan error, 1)
	go func() { errc <- c.Ping(context.Background()) }()
	b.expect([]byte{0xC0, 0x00})
	b.send([]byte{0xD0, 0x00})
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	go func() { errc <- c.Close() }()
	b.expect([]byte{0xE0, 0x00})
	_ = b.c.Close()
	<-errc
	if _, ok := <-c.Messages(); ok {
		t.Fatal("Messages stays open after Close")
	}
}

func TestTopicChecks(t *testing.T) {
	c, _ := connect(t)
	if err := c.Publish("a/+", nil, false); err == nil {
		t.Error("a wildcard topic was published")
	}
	if err := c.Publish("", nil, false); err == nil {
		t.Error("an empty topic was published")
	}
	if _, err := c.Subscribe(context.Background(), ""); err == nil {
		t.Error("an empty filter was subscribed")
	}
}
