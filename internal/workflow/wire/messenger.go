package wire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/workflow/source"
)

// Messenger delivers a message write's text and a notify target's
// summary (plan §3.2, §3.4): "os" is a desktop note, "web" an attention
// event for the browsers, "${secret:NAME}" a webhook whose URL is that
// secret (https only), posted as JSON {"text": …}. The URL never shows
// in an error.
type Messenger struct {
	OS, Web func(ctx context.Context, text string) error
	Secrets source.SecretGetter
	Post    func(ctx context.Context, url string, body []byte) error
}

// Send delivers text via one channel.
func (ms Messenger) Send(ctx context.Context, via, text string) error {
	switch via = strings.TrimSpace(via); via {
	case "os", "web":
		f := ms.OS
		if via == "web" {
			f = ms.Web
		}
		if f == nil {
			return fmt.Errorf("message via %s is not available here", via)
		}
		return f(ctx, text)
	}
	if !strings.HasPrefix(via, "${") {
		return fmt.Errorf("message via %q: an MCP server is not a message channel yet; use a write to: mcp step with the server's send tool", via)
	}
	name, ok := strings.CutPrefix(via, "${secret:")
	name, closed := strings.CutSuffix(name, "}")
	if !ok || !closed || secrets.ValidName(name) != nil {
		return errors.New("message via a webhook: use ${secret:NAME}, where the secret holds the https URL")
	}
	if ms.Secrets == nil {
		return fmt.Errorf("message via ${secret:%s}: no secret store", name)
	}
	url, ok, err := ms.Secrets.Get(name)
	switch {
	case err != nil:
		return fmt.Errorf("message via ${secret:%s}: the secret could not be read", name)
	case !ok || strings.TrimSpace(url) == "":
		return fmt.Errorf("message via ${secret:%s}: the secret is not set", name)
	case !strings.HasPrefix(strings.TrimSpace(url), "https://"):
		return fmt.Errorf("message via ${secret:%s}: the webhook must be an https URL", name)
	case ms.Post == nil:
		return errors.New("message via a webhook is not available here")
	}
	body, _ := json.Marshal(map[string]string{"text": text})
	if err := ms.Post(ctx, strings.TrimSpace(url), body); err != nil {
		return fmt.Errorf("message via ${secret:%s}: %s", name, strings.ReplaceAll(err.Error(), strings.TrimSpace(url), "<webhook>"))
	}
	return nil
}
