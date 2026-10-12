package session

import "bytes"

// DetachSeq is a private APC string the UI writes to its terminal to ask
// the session server to detach the current client ("Session: Detach").
// The server removes it from the output, so it never reaches the client
// terminal.
const DetachSeq = "\x1b_termocode:detach\x1b\\"

// detachFilter finds detach requests in the UI output.
type detachFilter struct {
	// held is the end of the last read when it may be the start of a
	// DetachSeq split across reads.
	held []byte
}

// Filter returns p without detach requests and reports whether there was
// one. A request split across reads is held back until it is complete.
func (f *detachFilter) Filter(p []byte) ([]byte, bool) {
	data := p
	if len(f.held) > 0 {
		data = append(f.held, p...)
		f.held = nil
	}
	seq := []byte(DetachSeq)
	found := false
	var out []byte
	for {
		i := bytes.Index(data, seq)
		if i < 0 {
			break
		}
		found = true
		out = append(out, data[:i]...)
		data = data[i+len(seq):]
	}
	// Hold back a tail that may be the start of a request.
	for k := min(len(data), len(seq)-1); k > 0; k-- {
		if bytes.HasPrefix(seq, data[len(data)-k:]) {
			f.held = append([]byte(nil), data[len(data)-k:]...)
			data = data[:len(data)-k]
			break
		}
	}
	if !found {
		return data, false
	}
	return append(out, data...), true
}
