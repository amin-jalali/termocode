package session

import (
	"bytes"
	"encoding/base64"
	"path/filepath"
	"strconv"
	"strings"
)

// The web hand-off of `termocode ssh --web` (WEB-29). The far
// `termocode --link --web=<port>` asks its UI to start the web panel on a
// unix socket and tells the local `termocode ssh` where it is, in-band
// inside the SSH channel, as one private APC string no terminal shows:
//
//	ESC _ termocode:web;1;<port>;<b64url socket path>;<b64url secret> ESC \
//	ESC _ termocode:web;0;<b64url reason> ESC \          (it did not start)
//
// The local side removes it from the output (it never reaches the
// terminal or its scrollback), forwards 127.0.0.1:<port> to the socket
// with a second ssh and opens the one-time link in the browser here.

// WebHandoff is what the far termocode tells `termocode ssh --web`.
type WebHandoff struct {
	Port   int    // the local port the far gateway answers to
	Sock   string // the far gateway's unix socket
	Secret string // the one-time launch secret
	Err    string // set instead when the far side could not start the panel
}

const (
	webAPCPrefix = "\x1b_termocode:web;"
	apcEnd       = "\x1b\\"
	// maxWebAPC bounds a hand-off string (and what the filter holds).
	maxWebAPC = 512
	// maxSockPath is a unix socket path's limit on every system we run on.
	maxSockPath = 104
	maxSecret   = 128
	maxWebErr   = 200
)

var b64 = base64.RawURLEncoding

// WebHandoffAPC encodes h for the far side's output.
func WebHandoffAPC(h WebHandoff) string {
	if h.Err != "" {
		return webAPCPrefix + "0;" + b64.EncodeToString([]byte(h.Err)) + apcEnd
	}
	return webAPCPrefix + "1;" + strconv.Itoa(h.Port) + ";" + b64.EncodeToString([]byte(h.Sock)) + ";" +
		b64.EncodeToString([]byte(h.Secret)) + apcEnd
}

// parseWebHandoff reads the payload between the prefix and the end; ok is
// false for anything that is not a well-formed hand-off.
func parseWebHandoff(p []byte) (WebHandoff, bool) {
	parts := strings.Split(string(p), ";")
	switch {
	case len(parts) == 2 && parts[0] == "0":
		msg, err := b64.DecodeString(parts[1])
		if err != nil || len(msg) == 0 || len(msg) > maxWebErr {
			return WebHandoff{}, false
		}
		return WebHandoff{Err: printable(string(msg))}, true
	case len(parts) == 4 && parts[0] == "1":
		port, err := strconv.Atoi(parts[1])
		if err != nil || port < 1024 || port > 65535 {
			return WebHandoff{}, false
		}
		sock, err1 := b64.DecodeString(parts[2])
		secret, err2 := b64.DecodeString(parts[3])
		if err1 != nil || err2 != nil || !goodSockPath(string(sock)) || !goodSecret(string(secret)) {
			return WebHandoff{}, false
		}
		return WebHandoff{Port: port, Sock: string(sock), Secret: string(secret)}, true
	}
	return WebHandoff{}, false
}

func goodSockPath(s string) bool {
	return len(s) > 1 && len(s) <= maxSockPath && filepath.IsAbs(s) && filepath.Clean(s) == s &&
		!strings.ContainsAny(s, "\x00\n\r:")
}

// goodSecret: the launch secret is base64url text (webgw.NewLinkSecret).
func goodSecret(s string) bool {
	if s == "" || len(s) > maxSecret {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// printable drops control characters (the reason is shown in a terminal).
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

// webFilter removes hand-off strings from a byte stream and returns the
// good ones. A string split across reads is held back until it ends
// (or turns out too long to be one: then it passes as it is). Unlike
// seqFilter on the key side, it holds a possible prefix from its first
// byte, a lone Esc too: it reads the far UI's output, where the rest of
// a sequence follows at once, and a split right after the Esc must not
// let a secret through to the terminal.
type webFilter struct {
	held []byte
}

func newWebFilter() *webFilter { return &webFilter{} }

// Filter returns p without the hand-off strings and the good hand-offs
// found, in order.
func (f *webFilter) Filter(p []byte) ([]byte, []WebHandoff) {
	data := p
	if len(f.held) > 0 {
		data = append(f.held, p...)
		f.held = nil
	}
	prefix := []byte(webAPCPrefix)
	var out []byte
	var found []WebHandoff
	start := 0 // data[start:i] is plain output not yet copied
	for i := 0; i < len(data); i++ {
		if data[i] != 0x1b {
			continue
		}
		rest := data[i:]
		if !bytes.HasPrefix(rest, prefix) {
			if len(rest) < len(prefix) && bytes.HasPrefix(prefix, rest) {
				out = append(out, data[start:i]...)
				f.held = append([]byte(nil), rest...)
				return out, found
			}
			continue
		}
		body := rest[len(prefix):]
		end := bytes.Index(body, []byte(apcEnd))
		if end < 0 || end > maxWebAPC {
			if end < 0 && len(body) <= maxWebAPC {
				out = append(out, data[start:i]...)
				f.held = append([]byte(nil), rest...)
				return out, found
			}
			continue // too long to be ours: let it through
		}
		out = append(out, data[start:i]...)
		if h, ok := parseWebHandoff(body[:end]); ok {
			found = append(found, h)
		}
		i += len(prefix) + end + len(apcEnd) - 1
		start = i + 1
	}
	if found == nil && start == 0 {
		return data, nil
	}
	return append(out, data[start:]...), found
}
