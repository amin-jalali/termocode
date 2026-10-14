package transfer

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

// Errors shared by the protocols.
var (
	ErrTimeout  = errors.New("the terminal did not answer")
	ErrCanceled = errors.New("canceled")
	ErrTooBig   = errors.New("too big")
)

// B64 is standard base64 of s.
func B64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// unB64 decodes standard base64 with or without padding.
func unB64(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
}

// ParseOSC returns the payload (the part after "<num>;") of every
// complete OSC <num> in stream. Both terminators (BEL and ESC \) count.
func ParseOSC(stream []byte, num string) [][]byte {
	prefix := []byte("\x1b]" + num + ";")
	var out [][]byte
	for {
		i := bytes.Index(stream, prefix)
		if i < 0 {
			return out
		}
		rest := stream[i+len(prefix):]
		end, tl := oscEnd(rest)
		if end < 0 {
			return out
		}
		out = append(out, rest[:end])
		stream = rest[end+tl:]
	}
}

// oscEnd finds the OSC terminator in b: its index and length, or -1.
func oscEnd(b []byte) (int, int) {
	for i, c := range b {
		switch c {
		case 0x07:
			return i, 1
		case 0x1b:
			if i+1 < len(b) && b[i+1] == '\\' {
				return i, 2
			}
			if i+1 == len(b) {
				return -1, 0
			}
		}
	}
	return -1, 0
}

// ITermFile is the iTerm2 sequence that saves data as name in the user's
// Downloads folder (inline=0: do not show it).
func ITermFile(name string, data []byte) string {
	return "\x1b]1337;File=name=" + B64(name) + ";size=" + strconv.Itoa(len(data)) +
		";inline=0:" + base64.StdEncoding.EncodeToString(data) + "\x07"
}

// RequestUploadSeq asks iTerm2 to open its file picker and send the
// chosen files back as a base64 tar.gz.
const RequestUploadSeq = "\x1b]1337;RequestUpload=format=tgz\x07"

// DecodeITermFile reads back an ITermFile payload (from ParseOSC). For
// tests and the docs' fake terminal.
func DecodeITermFile(payload []byte) (name string, data []byte, err error) {
	s := string(payload)
	if !strings.HasPrefix(s, "File=") {
		return "", nil, errors.New("not a File sequence")
	}
	args, body, ok := strings.Cut(strings.TrimPrefix(s, "File="), ":")
	if !ok {
		return "", nil, errors.New("no data")
	}
	for _, kv := range strings.Split(args, ";") {
		if v, ok := strings.CutPrefix(kv, "name="); ok {
			b, err := unB64(v)
			if err != nil {
				return "", nil, err
			}
			name = string(b)
		}
	}
	data, err = base64.StdEncoding.DecodeString(body)
	return name, data, err
}
