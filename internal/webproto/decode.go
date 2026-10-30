package webproto

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

// MaxFrame is the read limit for one browser message: the 1 MiB paste and
// edit cap plus 4 KiB for the envelope.
const MaxFrame = 1<<20 + 4<<10

// Errors of the strict decoder.
var (
	ErrTooBig      = errors.New("webproto: message too big")
	ErrBadEncoding = errors.New("webproto: message is not valid UTF-8")
	ErrTrailing    = errors.New("webproto: data after the message")
	ErrBadKey      = errors.New("webproto: duplicate key or key that is not lower-case ASCII")
)

// DecodeClient decodes a browser → hub message strictly: the size is
// capped, the bytes must be UTF-8, the type must be a browser type, and an
// unknown field is an error (an older binary refuses a newer page loudly).
func DecodeClient(b []byte) (Msg, error) {
	return decodeStrict(b, clientKinds)
}

func decodeStrict(b []byte, kinds map[string]func() Msg) (Msg, error) {
	if len(b) > MaxFrame {
		return nil, ErrTooBig
	}
	if !utf8.Valid(b) {
		return nil, ErrBadEncoding
	}
	if err := checkKeys(b); err != nil {
		return nil, err
	}
	kind, err := peekKind(b)
	if err != nil {
		return nil, err
	}
	mk, ok := kinds[kind]
	if !ok {
		return nil, ErrUnknownKind
	}
	m := mk()
	if err := unmarshalStrict(b, m); err != nil {
		return nil, err
	}
	return m, nil
}

// unmarshalStrict decodes exactly one JSON value with unknown fields refused.
func unmarshalStrict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return ErrTrailing
	}
	return nil
}

// checkKeys walks the JSON once and refuses a duplicate key in any object
// and any key that is not lower-case printable ASCII. Go matches keys
// without case (also through Unicode folding, e.g. the Kelvin sign) and
// keeps the last duplicate; another reader may keep the first. Every key
// of this protocol is lower-case ASCII, so both tricks are simply refused.
func checkKeys(b []byte) error {
	return walkValue(json.NewDecoder(bytes.NewReader(b)))
}

func walkValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch d {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return err
			}
			k, _ := kt.(string)
			if seen[k] || !plainKey(k) {
				return ErrBadKey
			}
			seen[k] = true
			if err := walkValue(dec); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := walkValue(dec); err != nil {
				return err
			}
		}
	}
	_, err = dec.Token() // the closing delimiter
	return err
}

func plainKey(k string) bool {
	for i := 0; i < len(k); i++ {
		c := k[i]
		if c < 0x20 || c > 0x7e || (c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}
