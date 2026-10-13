package termimg

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

// probeQuery is written to the terminal in one go: a kitty graphics query
// (1×1 RGB pixel, a=q only checks support and stores nothing), a cell-size
// query (CSI 16 t → CSI 6;h;w t) and DA1 last. Every terminal answers DA1, so
// its reply marks the end of all replies.
const probeQuery = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\" + "\x1b[16t" + "\x1b[c"

// tmuxPassthrough reports tmux's allow-passthrough option. A seam for tests.
var tmuxPassthrough = func() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "show", "-gv", "allow-passthrough").Output()
	if err != nil {
		return false
	}
	v := strings.TrimSpace(string(out))
	return v == "on" || v == "all"
}

// Probe finds what the terminal can draw. It must run before Bubble Tea
// takes the TTY (it switches in to raw mode and reads replies). When in or
// out is not a TTY, or the terminal does not answer within timeout, the
// environment hints decide.
func Probe(in, out *os.File, timeout time.Duration, getenv func(string) string) Caps {
	if getenv == nil {
		getenv = os.Getenv
	}
	envProto, envSure := DetectEnv(getenv)
	caps := Caps{Protocol: envProto}
	if envSure {
		caps.Reason = "terminal from environment: " + envProto.String()
	} else {
		caps.Reason = "no graphics hint in environment"
	}
	if getenv("TMUX") != "" {
		caps.InTmux = true
		caps.Passthrough = tmuxPassthrough()
		if !caps.Passthrough {
			caps.Protocol = Blocks
			caps.Reason = "inside tmux without allow-passthrough"
			return caps
		}
		// Queries are not forwarded reliably through tmux; trust the
		// outer terminal's environment hints.
		caps.Reason = "inside tmux with passthrough; " + caps.Reason
		return caps
	}
	if in == nil || out == nil || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return caps
	}
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return caps
	}
	defer func() { _ = term.Restore(int(in.Fd()), state) }()
	if _, err := out.WriteString(probeQuery); err != nil {
		return caps
	}
	reply := readUntil(in, time.Now().Add(timeout), hasDA1)
	kitty, sixel, cw, ch, gotDA1 := parseProbeReply(reply)
	caps.CellW, caps.CellH = cw, ch
	switch {
	case kitty:
		caps.Protocol = Kitty
		caps.Reason = "kitty graphics query answered OK"
	case sixel:
		caps.Protocol = Sixel
		caps.Reason = "DA1 lists sixel (4)"
	case gotDA1 && !envSure:
		caps.Protocol = Blocks
		caps.Reason = "terminal answered but has no graphics support"
	case !gotDA1:
		caps.Reason += " (terminal did not answer the query)"
	}
	return caps
}

// Query writes q to the terminal (in raw mode) and reads its answer until
// done says it is all in or timeout passes. Like Probe it must run before
// Bubble Tea takes the TTY. nil when in or out is not a terminal.
func Query(in, out *os.File, q string, timeout time.Duration, done func([]byte) bool) []byte {
	if in == nil || out == nil || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return nil
	}
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return nil
	}
	defer func() { _ = term.Restore(int(in.Fd()), state) }()
	if _, err := out.WriteString(q); err != nil {
		return nil
	}
	return readUntil(in, time.Now().Add(timeout), done)
}

// readUntil reads until done (for Probe: the DA1 reply "\x1b[?…c") is true
// or the deadline passes. readSome is platform specific (poll on unix).
func readUntil(in *os.File, deadline time.Time, done func([]byte) bool) []byte {
	var buf []byte
	tmp := make([]byte, 256)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return buf
		}
		n, ok := readSome(in, tmp, left)
		if !ok {
			return buf
		}
		buf = append(buf, tmp[:n]...)
		if done(buf) {
			return buf
		}
	}
}

func hasDA1(b []byte) bool {
	i := bytes.Index(b, []byte("\x1b[?"))
	for i >= 0 {
		rest := b[i+3:]
		j := 0
		for j < len(rest) && (rest[j] >= '0' && rest[j] <= '9' || rest[j] == ';') {
			j++
		}
		if j < len(rest) && rest[j] == 'c' {
			return true
		}
		k := bytes.Index(rest, []byte("\x1b[?"))
		if k < 0 {
			return false
		}
		i = i + 3 + k
	}
	return false
}

// parseProbeReply decodes the terminal's answers to probeQuery.
func parseProbeReply(b []byte) (kitty, sixel bool, cellW, cellH int, gotDA1 bool) {
	s := string(b)
	// Kitty: ESC _ G i=31 ; OK ESC \   (errors carry another message)
	if i := strings.Index(s, "\x1b_G"); i >= 0 {
		rest := s[i+3:]
		if end := strings.Index(rest, "\x1b\\"); end >= 0 {
			body := rest[:end]
			if semi := strings.IndexByte(body, ';'); semi >= 0 {
				keys, msg := body[:semi], body[semi+1:]
				if strings.Contains(","+keys+",", ",i=31,") && msg == "OK" {
					kitty = true
				}
			}
		}
	}
	// Cell size: ESC [ 6 ; h ; w t
	if i := strings.Index(s, "\x1b[6;"); i >= 0 {
		rest := s[i+4:]
		if end := strings.IndexByte(rest, 't'); end >= 0 {
			parts := strings.Split(rest[:end], ";")
			if len(parts) == 2 {
				h, e1 := strconv.Atoi(parts[0])
				w, e2 := strconv.Atoi(parts[1])
				if e1 == nil && e2 == nil && w > 0 && h > 0 {
					cellW, cellH = w, h
				}
			}
		}
	}
	// DA1: ESC [ ? p1 ; p2 ; … c
	for i := strings.Index(s, "\x1b[?"); i >= 0; {
		rest := s[i+3:]
		j := 0
		for j < len(rest) && (rest[j] >= '0' && rest[j] <= '9' || rest[j] == ';') {
			j++
		}
		if j < len(rest) && rest[j] == 'c' {
			gotDA1 = true
			for _, p := range strings.Split(rest[:j], ";") {
				if p == "4" {
					sixel = true
				}
			}
			break
		}
		k := strings.Index(rest, "\x1b[?")
		if k < 0 {
			break
		}
		i = i + 3 + k
	}
	return
}
