package session

import (
	"bytes"
	"strconv"
)

// AttachedSeq returns the private APC string the UI puts at the start of
// its first row once it has switched to the caps of attach number gen
// (see ClientCaps.Gen). The server removes it from the output and, after
// a client attached, sends that client nothing older than it: output
// drawn for the previous terminal (24-bit colours, kitty images) never
// reaches the new one.
func AttachedSeq(gen int) string {
	return attachedPrefix + strconv.Itoa(gen) + "\x1b\\"
}

const attachedPrefix = "\x1b_termocode:attached;"

// attachedMark is one AttachedSeq found in the output: pos is where it was
// in the filtered output.
type attachedMark struct {
	pos, gen int
}

// attachedFilter removes AttachedSeq strings from the UI output.
type attachedFilter struct {
	held []byte // a tail that may be the start of an AttachedSeq
}

// maxGenDigits bounds the number in an AttachedSeq; a longer one is not
// ours and passes through.
const maxGenDigits = 12

// Filter returns p without AttachedSeq strings and where they were. A
// string split across reads is held back until it is complete.
func (f *attachedFilter) Filter(p []byte) ([]byte, []attachedMark) {
	data := p
	if len(f.held) > 0 {
		data = append(f.held, p...)
		f.held = nil
	}
	pre := []byte(attachedPrefix)
	var out []byte
	var marks []attachedMark
	for {
		i := bytes.Index(data, pre)
		if i < 0 {
			break
		}
		rest := data[i+len(pre):]
		j := 0
		for j < len(rest) && j <= maxGenDigits && rest[j] >= '0' && rest[j] <= '9' {
			j++
		}
		switch {
		case j == len(rest) || (j < len(rest) && rest[j] == '\x1b' && j+1 == len(rest)):
			if j <= maxGenDigits {
				// Incomplete: hold it back.
				out = append(out, data[:i]...)
				f.held = append([]byte(nil), data[i:]...)
				return out, marks
			}
		case j > 0 && j <= maxGenDigits && j+1 < len(rest) && rest[j] == '\x1b' && rest[j+1] == '\\':
			out = append(out, data[:i]...)
			gen, _ := strconv.Atoi(string(rest[:j]))
			marks = append(marks, attachedMark{pos: len(out), gen: gen})
			data = rest[j+2:]
			continue
		}
		// Not ours: pass the prefix through.
		out = append(out, data[:i+len(pre)]...)
		data = rest
	}
	// Hold back a tail that may be the start of the prefix.
	for k := min(len(data), len(pre)-1); k > 0; k-- {
		if bytes.HasPrefix(pre, data[len(data)-k:]) {
			f.held = append([]byte(nil), data[len(data)-k:]...)
			data = data[:len(data)-k]
			break
		}
	}
	return append(out, data...), marks
}
