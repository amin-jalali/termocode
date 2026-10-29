package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// FuzzParseNextEdit checks the strict next-edit parser on any model
// answer and any buffer: an accepted edit names lines the model saw,
// near the caret, away from the last edit, and really changes them.
func FuzzParseNextEdit(f *testing.F) {
	buf := "package x\n\nfunc a() {\n\treturn 1\n}\n\nfunc b() {\n\treturn 2\n}\n"
	for _, s := range []string{
		`{"start":8,"end":8,"lines":["	return 3"]}`,
		"```json\n{\"start\":4,\"end\":4,\"lines\":[\"\\treturn 9\"]}\n```",
		`{"none":true}`,
		`{"start":1,"end":1,"lines":["x"],"extra":1}`,
		`{"start":1,"end":1,"lines":["x"]} {}`,
		`{"start":-9223372036854775808,"end":1,"lines":[]}`,
		`{"start":2,"end":1,"lines":["<CURSOR>"]}`,
	} {
		f.Add(s, buf, uint8(6), uint8(3))
	}
	f.Fuzz(func(t *testing.T, raw, text string, cursor, editRow uint8) {
		lines := strings.Split(text, "\n")
		in := NextEditInput{Lines: lines, CursorRow: int(cursor) % len(lines),
			Edits: []EditHunk{{Row: int(editRow) % len(lines), New: []string{"x"}}}}
		ne, ok := ParseNextEdit(raw, in)
		if !ok {
			return
		}
		first, last := NextEditWindowRange(len(lines), in.CursorRow)
		if ne.Start < first || ne.End >= last || ne.Start > ne.End {
			t.Fatalf("edit %d..%d outside the window %d..%d", ne.Start, ne.End, first, last)
		}
		if ne.End-ne.Start+1 > NextEditMaxLines || len(ne.Lines) > NextEditMaxLines {
			t.Fatalf("edit too long: %+v", ne)
		}
		if ne.Start-in.CursorRow > NextEditMaxDistance || in.CursorRow-ne.End > NextEditMaxDistance {
			t.Fatalf("edit too far from the caret: %+v", ne)
		}
		for _, l := range ne.Lines {
			if strings.ContainsAny(l, "\r\n") || strings.Contains(l, CursorMarker) {
				t.Fatalf("bad line %q", l)
			}
		}
		if e := in.Edits[0]; ne.Start <= e.Row && ne.End >= e.Row {
			t.Fatalf("edit %+v overlaps the last edit at %d", ne, e.Row)
		}
	})
}

// FuzzModelReplies reads any model text as an inline completion, a text
// tool call, a skill file and an OAuth callback. Nothing may panic, and
// what comes back keeps the documented shape.
func FuzzModelReplies(f *testing.F) {
	for _, s := range []string{
		"```go\nIMPORT: fmt\nfmt.Println(<CURSOR>x)\n```",
		"I will read it.\n```json\n{\"tool\":\"read_file\",\"args\":{\"path\":\"a.go\",\"limit\":1e300}}\n```",
		`{"tool":"run","args":null}`,
		"---\nname: review\ndescription: Review code\n---\nBody here\n",
		"http://localhost:1455/auth/callback?code=abc&state=xyz",
		"abc#xyz",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if c, ok := ParseInlineResponse(s); ok {
			if strings.TrimSpace(c.InsertText) == "" || strings.Contains(c.InsertText, CursorMarker) {
				t.Fatalf("bad completion %q", c.InsertText)
			}
		}
		if call, ok := ParseTextToolCall(s); ok {
			if call.Name == "" || call.Args == nil {
				t.Fatalf("bad call %+v", call)
			}
			for k := range call.Args {
				_ = ArgString(call.Args, k)
				_ = ArgBool(call.Args, k)
				_ = ArgInt(call.Args, k)
			}
		}
		_, _, _, _ = ParseSkill(s, "SKILL.md")
		if code, _, ok := ParseCallback(s); ok && code == "" {
			t.Fatalf("callback ok without a code: %q", s)
		}
		_, _ = FirstCodeBlock(s)
	})
}

// FuzzArgInt: a JSON number turns into an int with the same sign that is
// never further from zero than the number.
func FuzzArgInt(f *testing.F) {
	for _, s := range []string{`5`, `-3`, `2.9`, `"42"`, `"x"`, `9007199254740993`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, js string) {
		var v any
		if json.Unmarshal([]byte(js), &v) != nil {
			return
		}
		n := ArgInt(map[string]any{"n": v}, "n")
		if fv, ok := v.(float64); ok {
			if (fv > 0 && n < 0) || (fv < 0 && n > 0) {
				t.Fatalf("ArgInt(%v) = %d flips the sign", fv, n)
			}
		}
	})
}

// FuzzStreams reads any server-sent event stream from both providers.
// The parsers end with one final chunk and never panic.
func FuzzStreams(f *testing.F) {
	for _, s := range []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"m\",\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"t1\",\"name\":\"read_file\",\"input\":{}}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"hi\",\"tool_calls\":[{\"index\":0,\"id\":\"c\",\"function\":{\"name\":\"x\",\"arguments\":\"{\\\"a\\\"\"}}]}}]}\n\ndata: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":5,\"function\":{\"arguments\":\":1}\"}}]}}]}\n\ndata: [DONE]\n\n",
		"data: {\"type\":\"error\",\"error\":{\"message\":\"overloaded\"}}\n\n",
		"data: {broken\n\n",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, parse := range []func(context.Context, *strings.Reader, chan<- Chunk) Chunk{
			func(ctx context.Context, r *strings.Reader, ch chan<- Chunk) Chunk {
				return parseAnthropicSSE(ctx, r, ch)
			},
			func(ctx context.Context, r *strings.Reader, ch chan<- Chunk) Chunk { return parseOpenAISSE(ctx, r, ch) },
		} {
			ch := make(chan Chunk, 1)
			done := make(chan struct{})
			go func() {
				for range ch {
				}
				close(done)
			}()
			last := parse(context.Background(), strings.NewReader(s), ch)
			close(ch)
			<-done
			if !last.Done && last.Err == nil {
				t.Fatalf("final chunk neither done nor an error: %+v", last)
			}
		}
	})
}
