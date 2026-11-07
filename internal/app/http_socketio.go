package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/httpclient/socketio"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Socket.IO requests in .http files (API-20). The send key on
//
//	### chat
//	# @auth {"token": "{{token}}"}
//	# @listen 5s
//	SOCKETIO http://localhost:3000/chat
//
//	chat message {"text": "hi"}
//	===
//	["typing", true]
//
// connects over the WebSocket transport (Engine.IO v4), joins the URL's
// namespace with the @auth payload, emits each `===`-separated body
// message, keeps listening for @listen (default 3 s, at most 60 s) and
// shows the session as a read-only page. A message is `event` followed
// by one JSON argument (plain text is sent as a string), or a JSON
// array `["event", arg1, arg2]`.

const (
	httpSIOListenDefault = 3 * time.Second
	httpSIOListenMax     = 60 * time.Second
	httpSIOArgsMax       = 2000 // bytes of one event's arguments on the page
	httpSIOAckWait       = 5 * time.Second
)

// isSocketIORequest: method SOCKETIO (or SOCKET.IO).
func isSocketIORequest(r httpclient.Request) bool {
	return strings.EqualFold(r.Method, "SOCKETIO") || strings.EqualFold(r.Method, "SOCKET.IO")
}

// httpSIOEmit is one event to send.
type httpSIOEmit struct {
	event string
	args  []json.RawMessage
}

// httpSIOPlan is what one Socket.IO send does.
type httpSIOPlan struct {
	target socketio.Target
	auth   json.RawMessage
	header http.Header
	emits  []httpSIOEmit
	ack    bool // wait for each emit's ack
	listen time.Duration
}

// parseSIOEmit reads one body message.
func parseSIOEmit(s string) (httpSIOEmit, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") {
		var arr []json.RawMessage
		if err := json.Unmarshal([]byte(s), &arr); err != nil || len(arr) == 0 {
			return httpSIOEmit{}, fmt.Errorf("socket.io: %q is not a JSON array [\"event\", …]", clipSIO(s))
		}
		var name string
		if err := json.Unmarshal(arr[0], &name); err != nil || name == "" {
			return httpSIOEmit{}, fmt.Errorf("socket.io: the first item of %q must be the event name", clipSIO(s))
		}
		return httpSIOEmit{event: name, args: arr[1:]}, nil
	}
	name, rest, _ := strings.Cut(s, " ")
	if i := strings.IndexAny(name, "\t\n"); i >= 0 {
		name, rest = s[:i], s[i+1:]
	}
	rest = strings.TrimSpace(rest)
	e := httpSIOEmit{event: name}
	switch {
	case rest == "":
	case json.Valid([]byte(rest)):
		e.args = []json.RawMessage{json.RawMessage(rest)}
	default:
		b, _ := json.Marshal(rest)
		e.args = []json.RawMessage{b}
	}
	return e, nil
}

func clipSIO(s string) string {
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}

// httpSIOPlanFor reads the plan from a resolved request.
func httpSIOPlanFor(rr httpclient.Resolved) (httpSIOPlan, error) {
	t, err := socketio.ParseURL(rr.URL, rr.Directives["path"])
	if err != nil {
		return httpSIOPlan{}, err
	}
	p := httpSIOPlan{target: t, listen: httpSIOListenDefault, header: http.Header{}}
	for _, m := range httpclient.WSMessages(rr.Body) {
		e, err := parseSIOEmit(m)
		if err != nil {
			return httpSIOPlan{}, err
		}
		p.emits = append(p.emits, e)
	}
	if v := strings.TrimSpace(rr.Directives["auth"]); v != "" {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(v), &obj); err != nil {
			return httpSIOPlan{}, fmt.Errorf("socket.io: @auth must be a JSON object")
		}
		p.auth = json.RawMessage(v)
	}
	if v := strings.TrimSpace(rr.Directives["listen"]); v != "" {
		d, err := httpListenFor("socket.io", v, httpSIOListenMax)
		if err != nil {
			return httpSIOPlan{}, err
		}
		p.listen = d
	}
	_, p.ack = rr.Directives["ack"]
	for _, h := range rr.Headers {
		p.header.Add(h.Name, h.Value)
	}
	return p, nil
}

// httpSIODoneMsg is a finished Socket.IO session.
type httpSIODoneMsg struct {
	label string
	text  string
	err   error
}

// httpSocketIOSend sends the Socket.IO request req of file.
func (m *Model) httpSocketIOSend(file string, vars []httpclient.Var, req httpclient.Request) tea.Cmd {
	s := m.httpS()
	if s.sioRunning {
		return m.pushToastDetail(toast.Warn, "A Socket.IO session is still running", "It ends after its @listen time")
	}
	rv := httpResolver(file, vars, "")
	rr, un := rv.Resolve(req)
	if len(un) > 0 {
		return m.pushToastDetail(toast.Warn, "Socket.IO: unknown variable {{"+un[0].Name+"}}", "Define it in http-client.env.json or with @name = value")
	}
	plan, err := httpSIOPlanFor(rr)
	if err != nil {
		return m.pushToastDetail(toast.Warn, err.Error(), "")
	}
	s.sioRunning = true
	insecure := loadHTTPSettings().tlsInsecure()
	label := req.Label()
	secrets := append([]string(nil), rr.Secrets...)
	run := func() tea.Msg {
		text, err := httpSIOSession(context.Background(), plan, insecure, time.Now)
		for _, v := range secrets {
			if v != "" {
				text = strings.ReplaceAll(text, v, httpclient.Redacted)
				if err != nil {
					err = errors.New(strings.ReplaceAll(err.Error(), v, httpclient.Redacted))
				}
			}
		}
		return httpSIODoneMsg{label: label, text: text, err: err}
	}
	toastCmd := m.pushToastDetail(toast.Info, "Socket.IO: connecting to "+plan.target.String(), fmt.Sprintf("listening %s", plan.listen))
	return tea.Batch(toastCmd, run)
}

// httpSIOSession runs plan and returns its transcript. err is set when
// the session could not start; a drop while listening is in the text.
func httpSIOSession(ctx context.Context, plan httpSIOPlan, insecure bool, now func() time.Time) (string, error) {
	var b strings.Builder
	start := now()
	stamp := func() string { return fmt.Sprintf("%6.2fs", now().Sub(start).Seconds()) }
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	c, err := socketio.Dial(dctx, plan.target, socketio.Options{Auth: plan.auth, Header: plan.header, Insecure: insecure})
	cancel()
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Close() }()
	fmt.Fprintf(&b, "SOCKETIO %s  sid %s\n\n", plan.target.String(), c.SID())
	fmt.Fprintf(&b, "%s  connected to namespace %s\n", stamp(), plan.target.Namespace)
	for _, e := range plan.emits {
		fmt.Fprintf(&b, "%s  → %s%s\n", stamp(), e.event, httpSIOArgs(e.args))
		if plan.ack {
			actx, cancel := context.WithTimeout(ctx, httpSIOAckWait)
			ack, err := c.EmitAck(actx, e.event, e.args)
			cancel()
			if err != nil {
				fmt.Fprintf(&b, "%s  ack failed: %v\n", stamp(), err)
				continue
			}
			fmt.Fprintf(&b, "%s  ack%s\n", stamp(), httpSIOArgs(ack))
			continue
		}
		if err := c.Emit(ctx, e.event, e.args); err != nil {
			fmt.Fprintf(&b, "%s  emit failed: %v\n", stamp(), err)
		}
	}
	n := 0
	timer := time.NewTimer(plan.listen)
	defer timer.Stop()
listen:
	for {
		select {
		case ev, ok := <-c.Events():
			if !ok {
				if err := c.Err(); err != nil {
					fmt.Fprintf(&b, "%s  connection ended: %v\n", stamp(), err)
				}
				break listen
			}
			n++
			flag := ""
			if ev.Binary {
				flag = " (binary attachments not shown)"
			}
			fmt.Fprintf(&b, "%s  ← %s%s%s\n", stamp(), ev.Name, flag, httpSIOArgs(ev.Args))
		case <-timer.C:
			break listen
		case <-ctx.Done():
			break listen
		}
	}
	fmt.Fprintf(&b, "\n%d event(s) received in %s.\n", n, plan.listen)
	if d := c.Dropped(); d > 0 {
		fmt.Fprintf(&b, "%d more event(s) came too fast and were dropped.\n", d)
	}
	return b.String(), nil
}

// httpSIOArgs shows event arguments indented under the event line.
func httpSIOArgs(args []json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = string(a)
	}
	s := strings.Join(parts, ", ")
	if len(s) > httpSIOArgsMax {
		s = s[:httpSIOArgsMax] + fmt.Sprintf(" … (%d bytes)", len(s))
	}
	return "\n" + httpMQTTIndent(s)
}

// onHTTPSIODone shows the transcript.
func (m *Model) onHTTPSIODone(msg httpSIODoneMsg) tea.Cmd {
	m.httpS().sioRunning = false
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "Socket.IO: could not connect", msg.err.Error())
	}
	m.ensureEditorWindowCurrent()
	if err := m.openReadOnlyPage("Socket.IO: "+msg.label, msg.text, "", "text"); err != nil {
		return m.pushToastDetail(toast.Errr, "Could not open the Socket.IO page", err.Error())
	}
	m.focus = FocusEditor
	return nil
}
