// Package mqtt is a small MQTT 3.1.1 client for the HTTP client view:
// connect, publish (QoS 0), subscribe and read the messages that come
// in. It has no dependency and never logs the password.
package mqtt

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
)

// Packet types (high nibble of the first byte).
const (
	pConnect     = 1
	pConnack     = 2
	pPublish     = 3
	pPuback      = 4
	pSubscribe   = 8
	pSuback      = 9
	pPingreq     = 12
	pPingresp    = 13
	pDisconnect  = 14
	maxRemaining = 268435455
)

// Options are the CONNECT fields.
type Options struct {
	ClientID     string
	Username     string
	Password     string
	KeepAlive    time.Duration
	CleanSession bool
}

// Message is one PUBLISH from the broker.
type Message struct {
	Topic   string
	Payload []byte
	QoS     byte
	Retain  bool
}

// ErrRefused is wrapped by the error of a refused CONNECT.
var ErrRefused = errors.New("the broker refused the connection")

var refusals = map[byte]string{
	1: "unsupported protocol version",
	2: "client ID not allowed",
	3: "server unavailable",
	4: "bad user name or password",
	5: "not authorized",
}

// Client is one connection to a broker.
type Client struct {
	conn net.Conn
	r    *bufio.Reader
	wmu  sync.Mutex
	msgs chan Message

	mu      sync.Mutex
	nextID  uint16
	subacks map[uint16]chan byte
	pings   []chan struct{}
	err     error
	done    chan struct{}
	closed  bool
}

// Dial opens a TCP connection to addr ("host:port") and connects.
func Dial(ctx context.Context, addr string, o Options) (*Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	c, err := NewClient(ctx, conn, o)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

// NewClient sends CONNECT on conn and waits for the CONNACK (bounded by
// ctx). On success a reader runs until the connection ends.
func NewClient(ctx context.Context, conn net.Conn, o Options) (*Client, error) {
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	r := bufio.NewReader(conn)
	err := handshake(conn, r, o)
	if fired := !stop(); fired && err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	c := &Client{
		conn:    conn,
		r:       r,
		msgs:    make(chan Message, 64),
		subacks: map[uint16]chan byte{},
		done:    make(chan struct{}),
	}
	crash.Go("mqtt reader", c.read)
	return c, nil
}

func handshake(conn net.Conn, r *bufio.Reader, o Options) error {
	var b []byte
	b = appendString(b, "MQTT")
	b = append(b, 4) // protocol level 3.1.1
	var flags byte
	if o.CleanSession {
		flags |= 0x02
	}
	if o.Username != "" {
		flags |= 0x80
	}
	if o.Password != "" {
		flags |= 0x40
	}
	b = append(b, flags)
	ka := int(o.KeepAlive / time.Second)
	if ka > 0xFFFF {
		ka = 0xFFFF
	}
	b = binary.BigEndian.AppendUint16(b, uint16(ka))
	b = appendString(b, o.ClientID)
	if o.Username != "" {
		b = appendString(b, o.Username)
	}
	if o.Password != "" {
		b = appendString(b, o.Password)
	}
	if _, err := conn.Write(packet(pConnect<<4, b)); err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}
	h, body, err := readPacket(r)
	if err != nil {
		return fmt.Errorf("mqtt connect: no answer from the broker: %w", err)
	}
	if h>>4 != pConnack || len(body) != 2 {
		return errors.New("mqtt connect: the broker sent no CONNACK")
	}
	if code := body[1]; code != 0 {
		why := refusals[code]
		if why == "" {
			why = fmt.Sprintf("code %d", code)
		}
		return fmt.Errorf("%w: %s", ErrRefused, why)
	}
	return nil
}

// Messages are the PUBLISH packets the broker sends; closed when the
// connection ends.
func (c *Client) Messages() <-chan Message { return c.msgs }

// Err is why the connection ended (nil while it runs or after Close).
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *Client) write(p []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err := c.conn.Write(p)
	return err
}

func checkTopic(topic string, filter bool) error {
	switch {
	case topic == "":
		return errors.New("the topic is empty")
	case len(topic) > 0xFFFF:
		return errors.New("the topic is too long")
	case strings.ContainsRune(topic, 0):
		return errors.New("the topic has a NUL character")
	case !filter && strings.ContainsAny(topic, "+#"):
		return errors.New("a topic to publish to cannot have + or #")
	}
	return nil
}

// Publish sends payload to topic with QoS 0.
func (c *Client) Publish(topic string, payload []byte, retain bool) error {
	if err := checkTopic(topic, false); err != nil {
		return err
	}
	h := byte(pPublish << 4)
	if retain {
		h |= 1
	}
	b := appendString(nil, topic)
	b = append(b, payload...)
	if len(b) > maxRemaining {
		return errors.New("the message is too large")
	}
	return c.write(packet(h, b))
}

// Subscribe asks for the messages of filter (QoS 0) and waits for the
// SUBACK; it returns the QoS the broker granted.
func (c *Client) Subscribe(ctx context.Context, filter string) (byte, error) {
	if err := checkTopic(filter, true); err != nil {
		return 0, err
	}
	c.mu.Lock()
	c.nextID++
	if c.nextID == 0 {
		c.nextID = 1
	}
	id := c.nextID
	ch := make(chan byte, 1)
	c.subacks[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.subacks, id)
		c.mu.Unlock()
	}()
	b := binary.BigEndian.AppendUint16(nil, id)
	b = appendString(b, filter)
	b = append(b, 0)
	if err := c.write(packet(pSubscribe<<4|0x02, b)); err != nil {
		return 0, err
	}
	select {
	case q := <-ch:
		if q == 0x80 {
			return 0, fmt.Errorf("the broker refused the subscription to %s", filter)
		}
		return q, nil
	case <-c.done:
		return 0, errors.New("the connection ended")
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// Ping sends PINGREQ and waits for PINGRESP.
func (c *Client) Ping(ctx context.Context) error {
	ch := make(chan struct{}, 1)
	c.mu.Lock()
	c.pings = append(c.pings, ch)
	c.mu.Unlock()
	if err := c.write([]byte{pPingreq << 4, 0}); err != nil {
		return err
	}
	select {
	case <-ch:
		return nil
	case <-c.done:
		return errors.New("the connection ended")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close sends DISCONNECT and closes the connection.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_ = c.write([]byte{pDisconnect << 4, 0})
	err := c.conn.Close()
	<-c.done
	return err
}

func (c *Client) read() {
	defer close(c.msgs)
	defer close(c.done)
	for {
		h, body, err := readPacket(c.r)
		if err != nil {
			c.mu.Lock()
			if !c.closed && !errors.Is(err, io.EOF) {
				c.err = err
			}
			c.mu.Unlock()
			return
		}
		switch h >> 4 {
		case pPublish:
			m, id, err := parsePublish(h, body)
			if err != nil {
				c.mu.Lock()
				c.err = err
				c.mu.Unlock()
				_ = c.conn.Close()
				return
			}
			if m.QoS > 0 {
				_ = c.write(packet(pPuback<<4, binary.BigEndian.AppendUint16(nil, id)))
			}
			select {
			case c.msgs <- m:
			default: // a slow reader loses messages, never the connection
			}
		case pSuback:
			if len(body) >= 3 {
				id := binary.BigEndian.Uint16(body)
				c.mu.Lock()
				ch := c.subacks[id]
				c.mu.Unlock()
				if ch != nil {
					select {
					case ch <- body[2]:
					default: // a second SUBACK for the same id
					}
				}
			}
		case pPingresp:
			c.mu.Lock()
			if len(c.pings) > 0 {
				c.pings[0] <- struct{}{}
				c.pings = c.pings[1:]
			}
			c.mu.Unlock()
		}
	}
}

func parsePublish(h byte, body []byte) (Message, uint16, error) {
	m := Message{QoS: (h >> 1) & 3, Retain: h&1 == 1}
	if m.QoS > 2 {
		return m, 0, errors.New("the broker sent a bad QoS")
	}
	if len(body) < 2 {
		return m, 0, errors.New("the broker sent a short PUBLISH")
	}
	n := int(binary.BigEndian.Uint16(body))
	body = body[2:]
	if len(body) < n {
		return m, 0, errors.New("the broker sent a short PUBLISH")
	}
	m.Topic, body = string(body[:n]), body[n:]
	var id uint16
	if m.QoS > 0 {
		if len(body) < 2 {
			return m, 0, errors.New("the broker sent a short PUBLISH")
		}
		id, body = binary.BigEndian.Uint16(body), body[2:]
	}
	m.Payload = append([]byte(nil), body...)
	return m, id, nil
}

func appendString(b []byte, s string) []byte {
	b = binary.BigEndian.AppendUint16(b, uint16(len(s)))
	return append(b, s...)
}

func packet(h byte, body []byte) []byte {
	p := append([]byte{h}, encodeLength(len(body))...)
	return append(p, body...)
}

// encodeLength is the MQTT variable-length "remaining length".
func encodeLength(n int) []byte {
	var b []byte
	for {
		d := byte(n % 128)
		n /= 128
		if n > 0 {
			d |= 0x80
		}
		b = append(b, d)
		if n == 0 {
			return b
		}
	}
}

func readLength(r io.ByteReader) (int, error) {
	n, mul := 0, 1
	for i := 0; i < 4; i++ {
		d, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		n += int(d&0x7F) * mul
		if d&0x80 == 0 {
			return n, nil
		}
		mul *= 128
	}
	return 0, errors.New("bad remaining length")
}

// maxPacket caps one incoming packet (a broker cannot fill memory).
const maxPacket = 16 << 20

func readPacket(r *bufio.Reader) (byte, []byte, error) {
	h, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	n, err := readLength(r)
	if err != nil {
		return 0, nil, err
	}
	if n > maxPacket {
		return 0, nil, errors.New("the broker sent a packet over 16 MiB")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return h, body, nil
}
