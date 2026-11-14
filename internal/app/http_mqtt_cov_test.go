package app

import (
	"bufio"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/httpclient/mqtt"
)

// covMQTTBroker is a tiny MQTT 3.1.1 broker on loopback: it accepts
// every client, grants subscriptions (the filter "deny" is refused),
// sends a retained greeting and a big payload after a subscription,
// echoes every publish, and breaks the connection after subscribing to
// "drop".
func covMQTTBroker(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go covMQTTServe(c)
		}
	}()
	return l.Addr().String()
}

func covMQTTPacket(head byte, body []byte) []byte {
	out := []byte{head}
	n := len(body)
	for {
		d := byte(n % 128)
		n /= 128
		if n > 0 {
			d |= 0x80
		}
		out = append(out, d)
		if n == 0 {
			break
		}
	}
	return append(out, body...)
}

func covMQTTPublish(topic string, payload []byte, retain bool) []byte {
	head := byte(0x30)
	if retain {
		head |= 0x01
	}
	b := []byte{byte(len(topic) >> 8), byte(len(topic))}
	b = append(b, topic...)
	return covMQTTPacket(head, append(b, payload...))
}

func covMQTTServe(c net.Conn) {
	defer func() { _ = c.Close() }()
	r := bufio.NewReader(c)
	for {
		head, err := r.ReadByte()
		if err != nil {
			return
		}
		n, mul := 0, 1
		for {
			d, err := r.ReadByte()
			if err != nil {
				return
			}
			n += int(d&0x7F) * mul
			mul *= 128
			if d&0x80 == 0 {
				break
			}
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil {
			return
		}
		switch head >> 4 {
		case 1: // CONNECT
			_, _ = c.Write([]byte{0x20, 0x02, 0x00, 0x00})
		case 8: // SUBSCRIBE: id, then (len, filter, qos)
			tl := int(body[2])<<8 | int(body[3])
			filter := string(body[4 : 4+tl])
			code := byte(0x00)
			if filter == "deny" {
				code = 0x80
			}
			_, _ = c.Write([]byte{0x90, 0x03, body[0], body[1], code})
			switch filter {
			case "deny":
			case "drop": // a PUBLISH too short to hold its topic
				_, _ = c.Write([]byte{0x30, 0x01, 0x00})
				return
			default:
				_, _ = c.Write(covMQTTPublish("room/1", []byte("hello"), true))
				_, _ = c.Write(covMQTTPublish("room/big", []byte(strings.Repeat("b", httpMQTTPayloadMax+5)), false))
			}
		case 3: // PUBLISH (QoS 0): echo
			tl := int(body[0])<<8 | int(body[1])
			_, _ = c.Write(covMQTTPublish(string(body[2:2+tl]), body[2+tl:], false))
		case 12: // PINGREQ
			_, _ = c.Write([]byte{0xD0, 0x00})
		case 14: // DISCONNECT
			return
		}
	}
}

func covMQTTPlan(t *testing.T, url, body string, dirs map[string]string) httpMQTTPlan {
	t.Helper()
	p, err := httpMQTTPlanFor(httpMQTTResolved(url, body, dirs))
	if err != nil {
		t.Fatal(err)
	}
	p.clientID = "tc"
	return p
}

// A session subscribes, publishes the body messages, prints what comes
// back (retained flag, long payloads cut) and the count.
func TestCovHTTPMQTTSession(t *testing.T) {
	addr := covMQTTBroker(t)
	p := covMQTTPlan(t, "mqtt://"+addr+"/room/1", "on\n===\noff", map[string]string{"subscribe": "room/#, deny", "listen": "300ms"})
	text, err := httpMQTTSession(context.Background(), p, false, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"client tc", "connected", "subscribed room/# (QoS 0)", "subscribe deny failed", "→ room/1\n    on", "← room/1 (retained)\n    hello",
		"← room/1\n    off", "… (2005 bytes)", "message(s) received in 300ms."} {
		if !strings.Contains(text, want) {
			t.Errorf("transcript lacks %q:\n%s", want, text)
		}
	}
	// The broker drops the connection: the transcript says so.
	p = covMQTTPlan(t, "mqtt://"+addr+"/t", "", map[string]string{"subscribe": "drop", "listen": "10s"})
	text, err = httpMQTTSession(context.Background(), p, false, time.Now)
	if err != nil || !strings.Contains(text, "connection ended") || !strings.Contains(text, "0 message(s)") {
		t.Errorf("drop: %v\n%s", err, text)
	}
	// A cancelled context stops listening.
	ctx, cancel := context.WithCancel(context.Background())
	p = covMQTTPlan(t, "mqtt://"+addr+"/quiet", "", map[string]string{"listen": "60s"})
	done := make(chan error, 1)
	go func() {
		var err error
		text, err = httpMQTTSession(ctx, p, false, time.Now)
		done <- err
	}()
	time.AfterFunc(200*time.Millisecond, cancel) // a loaded machine may cancel the dial instead
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancel did not stop the session")
	}
	if err == nil && !strings.Contains(text, "message(s) received in 1m0s") {
		t.Errorf("cancelled:\n%s", text)
	}
	// Nobody listens: the session cannot start.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := l.Addr().String()
	_ = l.Close()
	p = covMQTTPlan(t, "mqtt://"+dead+"/t", "", nil)
	if _, err := httpMQTTSession(context.Background(), p, false, time.Now); err == nil {
		t.Error("dead broker: no error")
	}
	if _, err := mqtt.ParseURL("mqtt://" + dead); err != nil {
		t.Fatal(err)
	}
}

// The send key on an MQTT request: unknown variables and bad plans say
// why; a session runs once at a time and its transcript opens as a
// page with the password hidden.
func TestCovHTTPMQTTSendFromBuffer(t *testing.T) {
	m, root, _ := covHTTP(t)
	addr := covMQTTBroker(t)
	file := filepath.Join(root, "mq.http")
	src := "@pw = s3cret-pass\n" +
		"\n###\n# @name kitchen\n# @listen 200ms\nMQTT mqtt://u:{{pw}}@" + addr + "/room/1\n\nhi s3cret-pass\n" + // 3-8
		"\n###\n# @name unknown\nMQTT mqtt://" + addr + "/{{nope}}\n" + // 10-12
		"\n###\n# @name nothing\nMQTT mqtt://" + addr + "\n" // 14-16
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	loop := newCovHTTPLoop(t, m)
	covHTTPEdit(t, m, file, 12)
	covHTTPDispatch(t, m, "http-send")
	if !strings.Contains(covHTTPToast(m), "MQTT: unknown variable {{nope}}") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	covHTTPEdit(t, m, file, 16)
	covHTTPDispatch(t, m, "http-send")
	if !strings.Contains(covHTTPToast(m), "nothing to do") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	covHTTPEdit(t, m, file, 6)
	c := covHTTPDispatch(t, m, "http-send")
	if !m.httpS().mqttRunning || !strings.Contains(covHTTPToast(m), "MQTT: connecting to") {
		t.Fatalf("not running: %q", covHTTPToast(m))
	}
	covHTTPReset(m)
	m.httpSendBufferLine(0, 6)
	if !strings.Contains(covHTTPToast(m), "still running") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	loop.run(c, func() bool { return !m.httpS().mqttRunning })
	var name, text string
	_ = m.nvim.ExecLuaResult(`return vim.api.nvim_buf_get_name(0)`, &name)
	_ = m.nvim.ExecLuaResult(`return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '\n')`, &text)
	if !strings.HasSuffix(name, "MQTT: kitchen") || !strings.Contains(text, "→ room/1") || strings.Contains(text, "s3cret-pass") {
		t.Errorf("page %q:\n%s", name, text)
	}
	// A failed connect is a toast.
	covHTTPReset(m)
	m.httpS().mqttRunning = true
	m.handleHTTPMsg(httpMQTTDoneMsg{label: "x", err: context.DeadlineExceeded})
	if m.httpS().mqttRunning || !strings.Contains(covHTTPToast(m), "MQTT: could not connect") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
}
