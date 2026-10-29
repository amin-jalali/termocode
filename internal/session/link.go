package session

import "bytes"

// The link protocol runs in-band between `termocode ssh` (on the user's
// machine) and the `termocode --link` client it starts over ssh: private
// APC strings that no terminal shows. The remote client announces itself
// with linkHello, so an older termocode on the far side never gets a ping
// typed into its UI; then every linkPing on its input is answered with a
// linkPong on its output. Each side removes the strings it reads.
const (
	linkHello = "\x1b_termocode:link;1\x1b\\"
	linkPing  = "\x1b_termocode:ping\x1b\\"
	linkPong  = "\x1b_termocode:pong\x1b\\"
)

// seqFilter removes fixed strings from a byte stream and reports which it
// found, in order. A string split across reads is held back until it is
// complete (or turns out not to be one) — only from its third byte on
// ("\x1b_t"): a lone Esc or Alt+_ key must never wait for the next key.
type seqFilter struct {
	seqs []string
	held []byte
}

// minHeld is the shortest tail seqFilter holds back.
const minHeld = 3

func newSeqFilter(seqs ...string) *seqFilter { return &seqFilter{seqs: seqs} }

// Filter returns p without the strings and the index (in seqs) of each
// one found.
func (f *seqFilter) Filter(p []byte) ([]byte, []int) {
	data := p
	if len(f.held) > 0 {
		data = append(f.held, p...)
		f.held = nil
	}
	var out []byte
	var found []int
	start := 0 // data[start:i] is plain output not yet copied
	for i := 0; i < len(data); i++ {
		if data[i] != 0x1b {
			continue
		}
		rest := data[i:]
		matched, partial := -1, false
		for k, s := range f.seqs {
			if bytes.HasPrefix(rest, []byte(s)) {
				matched = k
				break
			}
			if len(rest) >= minHeld && len(rest) < len(s) && bytes.HasPrefix([]byte(s), rest) {
				partial = true
			}
		}
		switch {
		case matched >= 0:
			out = append(out, data[start:i]...)
			found = append(found, matched)
			i += len(f.seqs[matched]) - 1
			start = i + 1
		case partial:
			out = append(out, data[start:i]...)
			f.held = append([]byte(nil), rest...)
			return out, found
		}
	}
	if found == nil && start == 0 {
		return data, nil
	}
	return append(out, data[start:]...), found
}
