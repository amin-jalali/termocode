package chatbridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// DiscordMax is the longest text one Discord message holds.
const DiscordMax = 2000

// DiscordAPI is the REST API's address.
const DiscordAPI = "https://discord.com/api/v10"

// Discord is a bot on Discord's REST API: it reads new messages in its
// channels and posts the answers there. No gateway (websocket): the
// bridge polls; a message's text needs the Message Content intent on
// the bot, or a mention of it.
type Discord struct {
	Token    string
	Base     string // "": DiscordAPI
	Client   *http.Client
	Channels []string

	self  string            // the bot's own user id
	after map[string]string // the newest message id seen per channel
}

func (d *Discord) Name() string { return "discord" }

type dcMessage struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Author  struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Bot      bool   `json:"bot"`
	} `json:"author"`
}

func (d *Discord) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	base := d.Base
	if base == "" {
		base = DiscordAPI
	}
	u := strings.TrimRight(base, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	hdr := http.Header{
		"Authorization": {"Bot " + d.Token},
		"User-Agent":    {"DiscordBot (https://github.com/amin-jalali/termocode, 1)"},
	}
	return call(ctx, d.Client, method, u, hdr, body, out, d.Token, func(status int, b []byte, _ http.Header) *APIError {
		var e struct {
			Message    string  `json:"message"`
			RetryAfter float64 `json:"retry_after"`
		}
		_ = json.Unmarshal(b, &e)
		return &APIError{Status: status, Msg: e.Message, RetryAfter: time.Duration(e.RetryAfter * float64(time.Second))}
	})
}

// Start checks the token and marks the newest message of each channel
// as seen.
func (d *Discord) Start(ctx context.Context) error {
	if strings.TrimSpace(d.Token) == "" {
		return errors.New("no bot token")
	}
	if len(d.Channels) == 0 {
		return errors.New("no channel: name the channel ids with --channel")
	}
	var me struct {
		ID string `json:"id"`
	}
	if err := d.do(ctx, http.MethodGet, "/users/@me", nil, nil, &me); err != nil {
		return err
	}
	d.self = me.ID
	d.after = map[string]string{}
	for _, ch := range d.Channels {
		var last []dcMessage
		if err := d.do(ctx, http.MethodGet, "/channels/"+url.PathEscape(ch)+"/messages", url.Values{"limit": {"1"}}, nil, &last); err != nil {
			return err
		}
		d.after[ch] = "0"
		if len(last) > 0 {
			d.after[ch] = last[0].ID
		}
	}
	return nil
}

// Poll reads the messages after the newest one seen, oldest first.
func (d *Discord) Poll(ctx context.Context) ([]Message, error) {
	var out []Message
	for _, ch := range d.Channels {
		var got []dcMessage
		q := url.Values{"after": {d.after[ch]}, "limit": {"50"}}
		if err := d.do(ctx, http.MethodGet, "/channels/"+url.PathEscape(ch)+"/messages", q, nil, &got); err != nil {
			return out, err
		}
		slices.SortFunc(got, func(a, b dcMessage) int { return cmpSnowflake(a.ID, b.ID) })
		for _, m := range got {
			if cmpSnowflake(m.ID, d.after[ch]) > 0 {
				d.after[ch] = m.ID
			}
			if m.Author.Bot || m.Author.ID == d.self || strings.TrimSpace(m.Content) == "" {
				continue
			}
			out = append(out, Message{Chat: ch, User: m.Author.ID, UserName: m.Author.Username, Text: m.Content})
		}
	}
	return out, nil
}

// Send posts text to a channel, in parts when it is long; it never
// pings anyone.
func (d *Discord) Send(ctx context.Context, chat, text string) error {
	for _, p := range Split(text, DiscordMax) {
		body := map[string]any{"content": p, "allowed_mentions": map[string]any{"parse": []string{}}}
		if err := d.do(ctx, http.MethodPost, "/channels/"+url.PathEscape(chat)+"/messages", nil, body, nil); err != nil {
			return err
		}
	}
	return nil
}

// cmpSnowflake orders Discord ids (decimal numbers of any length).
func cmpSnowflake(a, b string) int {
	x, errA := strconv.ParseUint(a, 10, 64)
	y, errB := strconv.ParseUint(b, 10, 64)
	if errA == nil && errB == nil {
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	}
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}
