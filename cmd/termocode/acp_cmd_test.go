package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// CMP-84: `termocode acp` serves ACP on its stdio; --help prints the usage.
func TestRunACP(t *testing.T) {
	var out, errw bytes.Buffer
	if code := runACPIn([]string{"--help"}, strings.NewReader(""), &out, &errw, nil); code != 0 || !strings.Contains(out.String(), "Agent") {
		t.Fatalf("help: %d %q", code, out.String())
	}
	if code := runACPIn([]string{"bogus"}, strings.NewReader(""), &out, &errw, nil); code != 2 {
		t.Fatalf("bad arg exit %d", code)
	}
	out.Reset()
	stream := func(ctx context.Context, msgs []ai.Message) <-chan ai.Chunk {
		ch := make(chan ai.Chunk, 1)
		ch <- ai.Chunk{Done: true}
		close(ch)
		return ch
	}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}` + "\n")
	if code := runACPIn(nil, in, &out, &errw, stream); code != 0 {
		t.Fatalf("serve exit %d: %s", code, errw.String())
	}
	if !strings.Contains(out.String(), `"protocolVersion":1`) || !strings.Contains(out.String(), `"name":"termocode"`) {
		t.Fatalf("initialize answer %q", out.String())
	}
}
