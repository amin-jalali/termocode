package trigger

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Webhook limits.
const (
	MaxHookBody   = 256 << 10       // a request body
	HookPerMinute = 30              // requests per hook per minute
	SlackMaxSkew  = 5 * time.Minute // a Slack timestamp older than this is a replay
	recentKept    = 500             // delivery keys kept against retries
	HookPrefix    = "/hooks/"
)

// Hook is one inbound webhook of a workflow.
type Hook struct {
	Workflow string
	Trigger  int
	Name     string
	From     string // generic | github | slack | linear | jira
}

// Server takes POST /hooks/<name>. Every field must be set.
type Server struct {
	// Lookup finds the hook of a name (only trusted, on workflows).
	Lookup func(name string) (Hook, bool)
	// Secret is the signing secret of a hook, from internal/secrets
	// ("workflow-hook.<name>"); false: none set, so the hook is refused.
	Secret func(name string) (string, bool)
	// Deliver gets each new event; it must not block.
	Deliver func(Event)
	// Now is the clock (nil: time.Now).
	Now func() time.Time

	mu     sync.Mutex
	recent []string
	seen   map[string]bool
	rate   map[string][]time.Time
}

// Errors of a request, as the status they answer with.
var (
	errNoHook   = errors.New("no such hook")
	errBadSig   = errors.New("bad or missing signature")
	errTooMany  = errors.New("too many requests")
	errBadInput = errors.New("bad payload")
)

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// ServeHTTP handles one request. Answers never echo the body.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	name, ok := strings.CutPrefix(r.URL.Path, HookPrefix)
	if !ok || name == "" || strings.Contains(name, "/") {
		http.Error(w, errNoHook.Error(), http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxHookBody+1))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	if len(body) > MaxHookBody {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	ev, reply, err := s.Handle(name, r.Header, body)
	switch {
	case errors.Is(err, errNoHook):
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	case errors.Is(err, errBadSig):
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	case errors.Is(err, errTooMany):
		w.Header().Set("Retry-After", "60")
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	case err != nil:
		http.Error(w, errBadInput.Error(), http.StatusBadRequest)
		return
	}
	if reply != "" { // Slack's url_verification
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, reply)
		return
	}
	if ev != nil && s.Deliver != nil {
		s.Deliver(*ev)
	}
	w.WriteHeader(http.StatusAccepted)
}

// Handle checks and parses one request. ev is nil for a retry of a
// delivery already taken or an event of no interest; reply is a text to
// answer with instead (a Slack challenge).
func (s *Server) Handle(name string, h http.Header, body []byte) (ev *Event, reply string, err error) {
	hook, ok := s.Lookup(name)
	if !ok {
		return nil, "", errNoHook
	}
	secret, ok := s.Secret(name)
	if !ok || secret == "" {
		return nil, "", errBadSig
	}
	if err := verify(hook.From, secret, h, body, s.now()); err != nil {
		return nil, "", err
	}
	if !s.allow(name) {
		return nil, "", errTooMany
	}
	e, reply, err := parse(hook.From, h, body)
	if err != nil || reply != "" || e == nil {
		return nil, reply, err
	}
	e.Workflow, e.Trigger = hook.Workflow, hook.Trigger
	e.Key = "hook:" + name + ":" + e.Key
	if !s.fresh(e.Key) {
		return nil, "", nil
	}
	return e, "", nil
}

// allow is the rate limit of one hook.
func (s *Server) allow(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rate == nil {
		s.rate = map[string][]time.Time{}
	}
	now := s.now()
	keep := s.rate[name][:0]
	for _, t := range s.rate[name] {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	if len(keep) >= HookPerMinute {
		s.rate[name] = keep
		return false
	}
	s.rate[name] = append(keep, now)
	return true
}

// fresh reports whether key was not delivered before (a sender retry).
func (s *Server) fresh(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if s.seen[key] {
		return false
	}
	s.seen[key] = true
	s.recent = append(s.recent, key)
	if len(s.recent) > recentKept {
		delete(s.seen, s.recent[0])
		s.recent = s.recent[1:]
	}
	return true
}

// ── Signatures ────────────────────────────────────────────────────────────

func mac(secret string, parts ...[]byte) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	for _, p := range parts {
		m.Write(p)
	}
	return m.Sum(nil)
}

// hexEqual compares a hex signature with want in constant time.
func hexEqual(got string, want []byte) bool {
	b, err := hex.DecodeString(strings.TrimSpace(got))
	return err == nil && hmac.Equal(b, want)
}

// Sign is the X-Termocode-Signature value of body (for senders and tests).
func Sign(secret string, body []byte) string {
	return "sha256=" + hex.EncodeToString(mac(secret, body))
}

func verify(from, secret string, h http.Header, body []byte, now time.Time) error {
	switch from {
	case "github":
		if sig, ok := strings.CutPrefix(h.Get("X-Hub-Signature-256"), "sha256="); ok && hexEqual(sig, mac(secret, body)) {
			return nil
		}
		return errBadSig
	case "slack":
		ts := h.Get("X-Slack-Request-Timestamp")
		sec, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return errBadSig
		}
		if d := now.Sub(time.Unix(sec, 0)); d > SlackMaxSkew || d < -SlackMaxSkew {
			return errBadSig
		}
		if sig, ok := strings.CutPrefix(h.Get("X-Slack-Signature"), "v0="); ok && hexEqual(sig, mac(secret, []byte("v0:"+ts+":"), body)) {
			return nil
		}
		return errBadSig
	case "linear":
		if hexEqual(h.Get("Linear-Signature"), mac(secret, body)) {
			return nil
		}
		return errBadSig
	default: // generic, jira
		for _, k := range []string{"X-Termocode-Signature", "X-Hub-Signature-256", "X-Hub-Signature"} {
			if sig, ok := strings.CutPrefix(h.Get(k), "sha256="); ok && hexEqual(sig, mac(secret, body)) {
				return nil
			}
		}
		// A sender that cannot sign (some automation tools) may send the
		// secret as a bearer token; it still never reaches a run.
		if tok, ok := strings.CutPrefix(h.Get("Authorization"), "Bearer "); ok &&
			subtle.ConstantTimeCompare([]byte(strings.TrimSpace(tok)), []byte(secret)) == 1 {
			return nil
		}
		return errBadSig
	}
}

// ── Payloads ──────────────────────────────────────────────────────────────

func parse(from string, h http.Header, body []byte) (*Event, string, error) {
	switch from {
	case "github":
		return parseGitHub(h, body)
	case "slack":
		return parseSlack(body)
	case "linear":
		return parseLinear(body)
	case "jira":
		return parseJira(body)
	default:
		return parseGeneric(body)
	}
}

func bodyKey(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:8])
}

func parseGitHub(h http.Header, body []byte) (*Event, string, error) {
	var p struct {
		Action string `json:"action"`
		Repo   struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Issue *struct {
			Number int       `json:"number"`
			Title  string    `json:"title"`
			URL    string    `json:"html_url"`
			Body   string    `json:"body"`
			User   ghAuthor  `json:"user"`
			Labels []ghLabel `json:"labels"`
		} `json:"issue"`
		PR *struct {
			Number int      `json:"number"`
			Title  string   `json:"title"`
			URL    string   `json:"html_url"`
			Body   string   `json:"body"`
			User   ghAuthor `json:"user"`
			Head   struct {
				Ref string `json:"ref"`
			} `json:"head"`
		} `json:"pull_request"`
		Comment *struct {
			Body string   `json:"body"`
			URL  string   `json:"html_url"`
			User ghAuthor `json:"user"`
		} `json:"comment"`
		Label *ghLabel `json:"label"`
		Run   *struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Branch     string `json:"head_branch"`
			Conclusion string `json:"conclusion"`
			URL        string `json:"html_url"`
		} `json:"workflow_run"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, "", fmt.Errorf("%w: %v", errBadInput, err)
	}
	e := &Event{Source: "github", Repo: p.Repo.FullName, Key: h.Get("X-GitHub-Delivery")}
	if e.Key == "" {
		e.Key = bodyKey(body)
	}
	switch h.Get("X-GitHub-Event") {
	case "issues":
		if p.Issue == nil || (p.Action != "opened" && p.Action != "labeled") {
			return nil, "", nil
		}
		e.Kind, e.Number, e.Title, e.URL, e.Body, e.Author = KindIssueOpened, p.Issue.Number, p.Issue.Title, p.Issue.URL, p.Issue.Body, p.Issue.User.Login
		for _, l := range p.Issue.Labels {
			e.Labels = append(e.Labels, l.Name)
		}
		if p.Action == "labeled" {
			e.Kind = KindIssueLabeled
			if p.Label != nil {
				e.Labels = []string{p.Label.Name}
			}
		}
	case "pull_request":
		if p.PR == nil || p.Action != "opened" {
			return nil, "", nil
		}
		e.Kind, e.Number, e.Title, e.URL, e.Body, e.Author, e.Branch = KindPROpened, p.PR.Number, p.PR.Title, p.PR.URL, p.PR.Body, p.PR.User.Login, p.PR.Head.Ref
	case "issue_comment":
		if p.Comment == nil || p.Issue == nil || p.Action != "created" {
			return nil, "", nil
		}
		e.Kind, e.Number, e.Title, e.URL, e.Body, e.Author = KindMention, p.Issue.Number, p.Issue.Title, p.Comment.URL, p.Comment.Body, p.Comment.User.Login
	case "workflow_run":
		if p.Run == nil || p.Action != "completed" || p.Run.Conclusion != "failure" {
			return nil, "", nil
		}
		e.Kind, e.Title, e.URL, e.Branch = KindCIFailed, p.Run.Name, p.Run.URL, p.Run.Branch
	case "ping":
		return nil, "", nil
	default:
		e.Kind, e.Title = KindWebhook, h.Get("X-GitHub-Event")
	}
	return e, "", nil
}

func parseSlack(body []byte) (*Event, string, error) {
	var p struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		EventID   string `json:"event_id"`
		Event     struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			User    string `json:"user"`
			Channel string `json:"channel"`
			TS      string `json:"ts"`
			BotID   string `json:"bot_id"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, "", fmt.Errorf("%w: %v", errBadInput, err)
	}
	switch p.Type {
	case "url_verification":
		return nil, p.Challenge, nil
	case "event_callback":
	default:
		return nil, "", nil
	}
	if p.Event.BotID != "" { // never start on a bot's own message (loops)
		return nil, "", nil
	}
	key := p.EventID
	if key == "" {
		key = p.Event.Channel + ":" + p.Event.TS
	}
	return &Event{Kind: KindWebhook, Source: "slack", Key: key, Title: firstLine(p.Event.Text),
		Body: p.Event.Text, Author: p.Event.User}, "", nil
}

func parseLinear(body []byte) (*Event, string, error) {
	var p struct {
		Action string `json:"action"`
		Type   string `json:"type"`
		URL    string `json:"url"`
		TS     int64  `json:"webhookTimestamp"`
		Data   struct {
			ID          string    `json:"id"`
			Identifier  string    `json:"identifier"`
			Title       string    `json:"title"`
			Description string    `json:"description"`
			Body        string    `json:"body"`
			URL         string    `json:"url"`
			Labels      []ghLabel `json:"labels"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, "", fmt.Errorf("%w: %v", errBadInput, err)
	}
	e := &Event{Kind: KindWebhook, Source: "linear", Title: p.Data.Title, Body: p.Data.Description, URL: p.Data.URL}
	if p.Data.Identifier != "" {
		e.Title = p.Data.Identifier + " " + e.Title
	}
	if e.Body == "" {
		e.Body = p.Data.Body
	}
	if e.URL == "" {
		e.URL = p.URL
	}
	for _, l := range p.Data.Labels {
		e.Labels = append(e.Labels, l.Name)
	}
	e.Key = p.Type + ":" + p.Data.ID + ":" + p.Action + ":" + strconv.FormatInt(p.TS, 10)
	if p.Data.ID == "" {
		e.Key = bodyKey(body)
	}
	return e, "", nil
}

func parseJira(body []byte) (*Event, string, error) {
	var p struct {
		Event string `json:"webhookEvent"`
		TS    int64  `json:"timestamp"`
		Issue struct {
			Key    string `json:"key"`
			Self   string `json:"self"`
			Fields struct {
				Summary     string   `json:"summary"`
				Description any      `json:"description"` // text, or a document
				Labels      []string `json:"labels"`
			} `json:"fields"`
		} `json:"issue"`
		Comment struct {
			Body any `json:"body"`
		} `json:"comment"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, "", fmt.Errorf("%w: %v", errBadInput, err)
	}
	e := &Event{Kind: KindWebhook, Source: "jira", Title: strings.TrimSpace(p.Issue.Key + " " + p.Issue.Fields.Summary),
		URL: p.Issue.Self, Labels: p.Issue.Fields.Labels, Body: plainText(p.Issue.Fields.Description)}
	if c := plainText(p.Comment.Body); c != "" {
		e.Body = c
	}
	e.Key = p.Issue.Key + ":" + p.Event + ":" + strconv.FormatInt(p.TS, 10)
	if p.Issue.Key == "" {
		e.Key = bodyKey(body)
	}
	return e, "", nil
}

// plainText is the text of a string or of a rich-text document (the
// "text" leaves, in order).
func plainText(v any) string {
	var b strings.Builder
	var walk func(any)
	walk = func(v any) {
		if b.Len() > MaxBody {
			return
		}
		switch x := v.(type) {
		case string:
			b.WriteString(x)
		case map[string]any:
			if t, ok := x["text"].(string); ok {
				b.WriteString(t)
			}
			if c, ok := x["content"]; ok {
				walk(c)
				if x["type"] == "paragraph" {
					b.WriteByte('\n')
				}
			}
		case []any:
			for _, it := range x {
				walk(it)
			}
		}
	}
	walk(v)
	return strings.TrimSpace(b.String())
}

func parseGeneric(body []byte) (*Event, string, error) {
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, "", fmt.Errorf("%w: %v", errBadInput, err)
	}
	str := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := p[k].(string); ok && s != "" {
				return s
			}
		}
		return ""
	}
	e := &Event{Kind: KindWebhook, Source: "generic", Title: str("title", "summary", "subject", "text"),
		Body: str("body", "description", "text", "message"), URL: str("url", "link"), Key: str("key", "id")}
	if ls, ok := p["labels"].([]any); ok {
		for _, l := range ls {
			if s, ok := l.(string); ok {
				e.Labels = append(e.Labels, s)
			}
		}
	}
	if e.Key == "" {
		e.Key = bodyKey(body)
	}
	return e, "", nil
}
