package mcptest

import (
	"encoding/json"
	"strings"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// Prompts answers the fake's prompts/list, prompts/get and
// completion/complete. Prompts: review (file required, style), hello.
func Prompts(method string, params json.RawMessage) (any, *extproc.Error) {
	switch method {
	case "prompts/list":
		return map[string]any{"prompts": []any{
			map[string]any{"name": "review", "title": "Review a file", "description": "Review one file",
				"arguments": []any{
					map[string]any{"name": "file", "description": "The file", "required": true},
					map[string]any{"name": "style", "description": "short or long"},
				}},
			map[string]any{"name": "hello", "description": "Say hello"},
		}}, nil
	case "prompts/get":
		var p struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		_ = json.Unmarshal(params, &p)
		msg := func(role, text string) map[string]any {
			return map[string]any{"role": role, "content": map[string]any{"type": "text", "text": text}}
		}
		switch p.Name {
		case "hello":
			return map[string]any{"messages": []any{msg("user", "Hello!")}}, nil
		case "review":
			if p.Arguments["file"] == "" {
				return nil, &extproc.Error{Code: extproc.CodeInvalidParams, Message: "missing argument: file"}
			}
			style := p.Arguments["style"]
			if style == "" {
				style = "short"
			}
			return map[string]any{"description": "Review", "messages": []any{
				msg("user", "Review "+p.Arguments["file"]+" (style: "+style+")."),
				msg("assistant", "I will look at it."),
			}}, nil
		}
		return nil, &extproc.Error{Code: extproc.CodeInvalidParams, Message: "unknown prompt: " + p.Name}
	case "completion/complete":
		var p struct {
			Argument struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"argument"`
		}
		_ = json.Unmarshal(params, &p)
		all := map[string][]string{"file": {"main.go", "main_test.go", "readme.md"}, "style": {"short", "long"}}[p.Argument.Name]
		vals := []string{}
		for _, v := range all {
			if strings.HasPrefix(v, p.Argument.Value) {
				vals = append(vals, v)
			}
		}
		return map[string]any{"completion": map[string]any{"values": vals, "total": len(vals), "hasMore": false}}, nil
	}
	return nil, &extproc.Error{Code: extproc.CodeMethodNotFound, Message: "method not found: " + method}
}
