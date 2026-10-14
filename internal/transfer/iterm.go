package transfer

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// ReadITermUpload reads iTerm2's answer to RequestUploadSeq from the
// terminal input: "ok" or "abort" on the first line, then (after ok) the
// tar.gz as base64 lines, ended by an empty line. Ctrl+C (or Esc before
// the answer) cancels. More than maxBytes of data is ErrTooBig.
func ReadITermUpload(ctx context.Context, r io.Reader, maxBytes int64) ([]byte, error) {
	br := bufio.NewReader(r)
	var status strings.Builder
	for {
		if ctx.Err() != nil {
			return nil, ErrCanceled
		}
		c, err := br.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("upload: %w", err)
		}
		if c == 0x03 || (c == 0x1b && status.Len() == 0) {
			return nil, ErrCanceled
		}
		if c == '\r' || c == '\n' {
			if status.Len() == 0 {
				continue
			}
			break
		}
		status.WriteByte(c)
		if status.Len() > 64 {
			return nil, fmt.Errorf("upload: unexpected answer %q", status.String())
		}
	}
	switch strings.TrimSpace(status.String()) {
	case "ok":
	case "abort":
		return nil, ErrCanceled
	default:
		return nil, fmt.Errorf("upload: unexpected answer %q", status.String())
	}
	var b64 []byte
	limit := maxBytes/3*4 + 8
	lineLen := 0
	for {
		if ctx.Err() != nil {
			return nil, ErrCanceled
		}
		c, err := br.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("upload: %w", err)
		}
		switch {
		case c == 0x03:
			return nil, ErrCanceled
		case c == '\r':
		case c == '\n' && len(b64) == 0:
		case c == '\n':
			if lineLen == 0 {
				data, err := base64.StdEncoding.DecodeString(string(b64))
				if err != nil {
					return nil, fmt.Errorf("upload: bad data: %w", err)
				}
				return data, nil
			}
			lineLen = 0
		default:
			b64 = append(b64, c)
			lineLen++
			if maxBytes > 0 && int64(len(b64)) > limit {
				return nil, ErrTooBig
			}
		}
	}
}
