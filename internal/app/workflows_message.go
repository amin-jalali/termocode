package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/source"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// Messages of workflows (plan §3.2, §3.4, WFX-18): a message write step
// and a "message:<step id>" notify target go out through
// wire.Messenger: the OS note and the browsers' attention here, a
// webhook (${secret:NAME}) over https, an MCP server's send tool (Slack)
// on the step's channel. A notify target's summary to a
// webhook is a write to an outside system: it goes only when that
// step's writes are allowed for this file version.

// wfxWebhookPost posts a message's JSON to a webhook (tests swap it).
var wfxWebhookPost = func(ctx context.Context, url string, body []byte) error {
	c := httpclient.New(httpclient.Options{Timeout: 20 * time.Second})
	defer c.Close()
	b := string(body)
	resp := c.Send(ctx, httpclient.Resolved{Request: httpclient.Request{Method: "POST", URL: url,
		Headers: []httpclient.Header{{Name: "Content-Type", Value: "application/json"}}, Body: &b}}, httpclient.Hooks{})
	if resp.Err != "" {
		return errors.New(resp.Err)
	}
	if resp.Status < 200 || resp.Status > 299 {
		return fmt.Errorf("the webhook answered %d", resp.Status)
	}
	return nil
}

// wfxMessageSecrets is where a webhook's ${secret:NAME} is read (tests
// swap it).
var wfxMessageSecrets = func() (source.SecretGetter, error) {
	s, err := configureSecretStore()
	if err != nil || s == nil {
		return nil, err
	}
	return s, nil
}

// wfxMessageMsg: a run's message step sends via os or web (Update shows
// it).
type wfxMessageMsg struct{ id, via, text string }

// wfxNoteMsg is a short note for the user from a message's goroutine.
type wfxNoteMsg struct{ text string }

// wfxMessenger is the run's messenger; block hands os / web to Update.
func wfxMessenger(id string, block func(context.Context, tea.Msg) error) wire.Messenger {
	ms := wire.Messenger{Post: wfxWebhookPost, Cache: &wire.SendCache{},
		OS: func(ctx context.Context, text string) error {
			return block(ctx, wfxMessageMsg{id: id, via: "os", text: text})
		},
		Web: func(ctx context.Context, text string) error {
			return block(ctx, wfxMessageMsg{id: id, via: "web", text: text})
		},
	}
	if s, err := wfxMessageSecrets(); err == nil && s != nil {
		ms.Secrets = s
	}
	return ms
}

// wfxDeliverLocal shows a message on the OS or to the browsers.
func (m *Model) wfxDeliverLocal(via, text, runID string, ok bool) tea.Cmd {
	if via == "os" {
		return m.notifyOS(text)
	}
	if m.web != nil {
		kind := webproto.AttentionRunDone
		if !ok {
			kind = webproto.AttentionRunFailed
		}
		m.web.attention(kind, runID)
	}
	return nil
}

// wfxNotifyMessage sends the summary of rf through message step sid of
// e (a "message:<sid>" notify target).
func (m *Model) wfxNotifyMessage(e wire.Entry, rf *run.RunFile, sid string) tea.Cmd {
	if e.File == nil {
		return nil
	}
	step := e.File.StepByID(sid)
	if step == nil || step.Write == nil || step.Write.To != "message" {
		return nil
	}
	via, text := strings.TrimSpace(step.Write.Via), wfxSummary(e.Name, rf)
	if via == "os" || via == "web" {
		return m.wfxDeliverLocal(via, text, rf.ID, rf.State == run.RunOK)
	}
	if ok, _ := ledger.Allowed(m.wfxS().root, e.Name, e.Hash, sid); !ok {
		// A write to an outside system: only once the step is allowed.
		return m.aiInfo(e.Name + ": summary not sent to " + sid + "; first Allow for this workflow on its card")
	}
	ms := wfxMessenger(rf.ID, nil)
	if mc := newWFMCP(m.aiMCP(), m.aiMCPTrustAnnotations()); mc != nil {
		ms.Tools = mc
	}
	// The channel is the step's as written: a template in it needs the
	// run's values, which a summary does not have.
	msg := wire.Message{Via: via, Channel: step.Write.Channel, Tool: step.Write.Tool, Text: text}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := ms.Deliver(ctx, msg); err != nil {
			recordError("workflow " + e.Name + ": notify message:" + sid + ": " + err.Error())
			return wfxNoteMsg{text: e.Name + ": the summary was not sent to message:" + sid + ": " + agentCtlShort(err.Error())}
		}
		return nil
	}
}
