package main

import (
	"bytes"
	"io"
	"strings"
)

// shortenCoverLine cuts the package list that "go test -coverpkg" repeats
// on every "coverage: N% of statements in ..." line.
func shortenCoverLine(line string) string {
	const marker = "of statements in "
	i := strings.Index(line, marker)
	if i < 0 {
		return line
	}
	tail := ""
	if strings.HasSuffix(line, "\n") {
		tail = "\n"
	}
	return line[:i+len(marker)] + "-coverpkg" + tail
}

// lineFilter passes whole lines through f to w. Close writes the last
// unfinished line.
type lineFilter struct {
	w   io.Writer
	f   func(string) string
	buf []byte
}

func (l *lineFilter) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := string(l.buf[:i+1])
		l.buf = l.buf[i+1:]
		if _, err := io.WriteString(l.w, l.f(line)); err != nil {
			return len(p), err
		}
	}
}

func (l *lineFilter) Close() error {
	if len(l.buf) == 0 {
		return nil
	}
	_, err := io.WriteString(l.w, l.f(string(l.buf)))
	l.buf = nil
	return err
}
