package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// "headersCommand" (your own mcp.json only): a command line run with
// `sh -c` at each connect. It prints one JSON object of header names to
// values ({"Authorization": "Bearer …"}) for short-lived tokens. Its
// output is never logged or put into an error; every value is redacted
// from then on.

// HeadersCommandTimeout bounds one run.
var HeadersCommandTimeout = 10 * time.Second

// ProjectHeadersCmdWarning is the problem of a headersCommand in a
// project file.
const ProjectHeadersCmdWarning = "headersCommand is only allowed in your own mcp.json"

func runHeadersCommand(line, dir string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), HeadersCommandTimeout)
	defer cancel()
	cmd := spawn.CommandContext(ctx, "sh", "-c", line)
	cmd.Stdin = nil
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout // stderr is dropped: it may echo a token
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	defer clear(stdout.Bytes())
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("headersCommand timed out after %s", HeadersCommandTimeout)
	}
	if err != nil {
		return nil, fmt.Errorf("headersCommand failed: %v", err)
	}
	var out map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, errors.New("headersCommand must print one JSON object of header names to text values")
	}
	for _, v := range out {
		ai.RegisterSecret(v)
	}
	return out, nil
}
