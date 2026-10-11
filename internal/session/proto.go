package session

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Frame types. Every message on the socket is a frame:
// 1 byte type, 4 bytes big-endian payload length, payload.
const (
	// Client → server.
	frameHello  byte = 'H' // first frame of an attach: size (4 bytes)
	frameInput  byte = 'I' // keys typed in the client terminal
	frameResize byte = 'R' // new client size (4 bytes)
	frameKill   byte = 'K' // first frame of `termocode kill`
	frameStatus byte = 'S' // first frame of `termocode sessions`; also the JSON reply

	// Server → client.
	frameOutput   byte = 'O' // bytes the UI wrote to its terminal
	frameExit     byte = 'X' // the UI exited: exit code (4 bytes)
	frameDetached byte = 'D' // another client took over: message
)

// maxFrame bounds a payload so a broken peer cannot make us allocate
// without limit.
const maxFrame = 1 << 20

var errFrameTooBig = errors.New("session: frame too big")

// writeFrame writes one frame. Callers serialize writes on a connection.
func writeFrame(w io.Writer, typ byte, payload []byte) error {
	if len(payload) > maxFrame {
		return errFrameTooBig
	}
	buf := make([]byte, 5+len(payload))
	buf[0] = typ
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(payload)))
	copy(buf[5:], payload)
	_, err := w.Write(buf)
	return err
}

// readFrame reads one frame.
func readFrame(r io.Reader) (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[1:5])
	if n > maxFrame {
		return 0, nil, errFrameTooBig
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return hdr[0], payload, nil
}

// encodeSize packs a terminal size for Hello / Resize.
func encodeSize(cols, rows int) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint16(b[0:2], uint16(cols))
	binary.BigEndian.PutUint16(b[2:4], uint16(rows))
	return b
}

// decodeSize unpacks encodeSize.
func decodeSize(b []byte) (cols, rows int, err error) {
	if len(b) < 4 {
		return 0, 0, fmt.Errorf("session: short size payload (%d bytes)", len(b))
	}
	return int(binary.BigEndian.Uint16(b[0:2])), int(binary.BigEndian.Uint16(b[2:4])), nil
}

func encodeCode(code int) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(int32(code)))
	return b
}

func decodeCode(b []byte) int {
	if len(b) < 4 {
		return 1
	}
	return int(int32(binary.BigEndian.Uint32(b)))
}
