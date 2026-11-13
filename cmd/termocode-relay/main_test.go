package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/relay"
)

func TestParseRefusesAPublicPlainListener(t *testing.T) {
	for _, args := range [][]string{
		{"-listen", "0.0.0.0:8080"},
		{"-listen", ":8080"},
		{"-listen", "192.168.1.5:8080"},
		{"-tls-cert", "a.pem"},
		{"-tls-key", "a.key"},
		{"-listen", "127.0.0.1:x"},
		{"extra"},
	} {
		if _, err := parse(args, io.Discard); err == nil {
			t.Errorf("parse(%q) worked, want an error", args)
		}
	}
	for _, args := range [][]string{
		nil,
		{"-listen", "127.0.0.1:0"},
		{"-listen", "[::1]:9000"},
		{"-listen", ":443", "-tls-cert", "c.pem", "-tls-key", "k.pem"},
		{"-listen", "127.0.0.1:8787", "-trust-proxy"},
	} {
		if _, err := parse(args, io.Discard); err != nil {
			t.Errorf("parse(%q) = %v, want ok", args, err)
		}
	}
}

// syncBuf is a goroutine-safe log sink.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestRunServesTheRelayOnLoopbackAndLogsJSON(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pr, pw := io.Pipe()
	var logs syncBuf
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"-listen", "127.0.0.1:0"}, &logs, pw) }()
	addr, err := bufio.NewReader(pr).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, pr) }()
	addr = strings.TrimSpace(strings.TrimPrefix(addr, "termocode-relay: listening on "))
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("listening line = %q", addr)
	}
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz = %v %v", resp, err)
	}
	if h := resp.Header.Get("X-Content-Type-Options"); h != "nosniff" {
		t.Errorf("nosniff header = %q", h)
	}
	_ = resp.Body.Close()
	room, _ := relay.NewRoomID()
	gw, _, err := websocket.Dial(ctx, "ws://"+addr+relay.PathPrefix+room+"?side=gw", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gw.CloseNow() }()
	go func() { _, _, _ = gw.Read(ctx) }() // answers the relay's close at the end
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), `"kind":"connect"`) {
		if time.Now().After(deadline) {
			t.Fatalf("no connect line in the log: %q", logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if strings.Contains(logs.String(), room) {
		t.Fatal("the log holds the room id")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run = %v after the context ended", err)
	}
}
