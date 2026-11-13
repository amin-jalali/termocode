package chatbridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// TelegramMax is the longest text one Telegram message holds (runes;
// the API allows 4096).
const TelegramMax = 4000

// TelegramAPI is the Bot API's address.
const TelegramAPI = "https://api.telegram.org"

// Telegram is a bot on the Telegram Bot API (getUpdates long polling,
// sendMessage). The zero offset is set by Start.
type Telegram struct {
	Token  string
	Base   string        // "" : TelegramAPI
	Client *http.Client  // nil: a client with a timeout
	Wait   time.Duration // the long-poll wait (0: 25s)

	offset int64
}

func (t *Telegram) Name() string { return "telegram" }

type tgReply struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

type tgUpdateJSON struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		From struct {
			ID       int64  `json:"id"`
			IsBot    bool   `json:"is_bot"`
			Username string `json:"username"`
		} `json:"from"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

func (t *Telegram) do(ctx context.Context, method string, q url.Values, body any, out any) error {
	base := t.Base
	if base == "" {
		base = TelegramAPI
	}
	u := strings.TrimRight(base, "/") + "/bot" + t.Token + "/" + method
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	verb := http.MethodGet
	if body != nil {
		verb = http.MethodPost
	}
	var r tgReply
	err := call(ctx, t.Client, verb, u, nil, body, &r, t.Token, func(status int, b []byte, _ http.Header) *APIError {
		_ = json.Unmarshal(b, &r)
		return &APIError{Status: status, Msg: r.Description, RetryAfter: time.Duration(r.Parameters.RetryAfter) * time.Second}
	})
	if err != nil {
		return err
	}
	if !r.OK {
		return &APIError{Status: http.StatusOK, Msg: r.Description}
	}
	if out != nil {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return errors.New("the reply is not the JSON expected")
		}
	}
	return nil
}

// Start checks the token (getMe) and skips the updates waiting on the
// server.
func (t *Telegram) Start(ctx context.Context) error {
	if strings.TrimSpace(t.Token) == "" {
		return errors.New("no bot token")
	}
	if err := t.do(ctx, "getMe", nil, nil, nil); err != nil {
		return err
	}
	var ups []tgUpdateJSON
	if err := t.do(ctx, "getUpdates", url.Values{"offset": {"-1"}, "timeout": {"0"}}, nil, &ups); err != nil {
		return err
	}
	for _, u := range ups {
		t.offset = max(t.offset, u.UpdateID+1)
	}
	return nil
}

// Poll waits up to Wait for new messages.
func (t *Telegram) Poll(ctx context.Context) ([]Message, error) {
	wait := t.Wait
	if wait <= 0 {
		wait = 25 * time.Second
	}
	q := url.Values{"timeout": {strconv.Itoa(int(wait / time.Second))}, "allowed_updates": {`["message"]`}}
	if t.offset != 0 {
		q.Set("offset", strconv.FormatInt(t.offset, 10))
	}
	var ups []tgUpdateJSON
	if err := t.do(ctx, "getUpdates", q, nil, &ups); err != nil {
		return nil, err
	}
	var out []Message
	for _, u := range ups {
		t.offset = max(t.offset, u.UpdateID+1)
		m := u.Message
		if m == nil || m.From.IsBot || strings.TrimSpace(m.Text) == "" {
			continue
		}
		out = append(out, Message{Chat: strconv.FormatInt(m.Chat.ID, 10), User: strconv.FormatInt(m.From.ID, 10),
			UserName: m.From.Username, Text: m.Text})
	}
	return out, nil
}

// Send posts text to chat as plain text, in parts when it is long.
func (t *Telegram) Send(ctx context.Context, chat, text string) error {
	for _, p := range Split(text, TelegramMax) {
		if err := t.do(ctx, "sendMessage", nil, map[string]any{"chat_id": chat, "text": p}, nil); err != nil {
			return err
		}
	}
	return nil
}
