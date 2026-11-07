package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/httpclient/mqtt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// MQTT requests in .http files (API-20). The send key on
//
//	### kitchen
//	# @subscribe sensors/#
//	# @listen 5s
//	MQTT mqtts://user:{{pass}}@broker:8883/sensors/kitchen
//
//	{"temp": 21}
//	===
//	second message
//
// connects (TLS for mqtts://), subscribes to the @subscribe filters,
// publishes each `===`-separated body message to the URL's topic, keeps
// listening for @listen (default 3 s, at most 60 s) and shows the
// session as a read-only page. With no body and no @subscribe it
// listens on the URL's topic.

const (
	httpMQTTListenDefault = 3 * time.Second
	httpMQTTListenMax     = 60 * time.Second
	httpMQTTPayloadMax    = 2000 // bytes of one payload on the page
)

// isMQTTRequest: method MQTT, or an mqtt:// / mqtts:// URL.
func isMQTTRequest(r httpclient.Request) bool {
	if strings.EqualFold(r.Method, "MQTT") {
		return true
	}
	u := strings.ToLower(strings.TrimSpace(r.URL))
	return strings.HasPrefix(u, "mqtt://") || strings.HasPrefix(u, "mqtts://")
}

// httpMQTTPlan is what one MQTT send does.
type httpMQTTPlan struct {
	target   mqtt.Target
	clientID string
	subs     []string
	publish  []string
	retain   bool
	listen   time.Duration
}

// httpMQTTPlanFor reads the plan from a resolved request.
func httpMQTTPlanFor(rr httpclient.Resolved) (httpMQTTPlan, error) {
	t, err := mqtt.ParseURL(rr.URL)
	if err != nil {
		return httpMQTTPlan{}, err
	}
	p := httpMQTTPlan{target: t, listen: httpMQTTListenDefault, publish: httpclient.WSMessages(rr.Body)}
	for _, f := range strings.FieldsFunc(rr.Directives["subscribe"], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		p.subs = append(p.subs, f)
	}
	if v := strings.TrimSpace(rr.Directives["listen"]); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			secs, aerr := strconv.Atoi(v)
			if aerr != nil {
				return httpMQTTPlan{}, fmt.Errorf("mqtt: @listen %q is not a duration (5s, 1m)", v)
			}
			d = time.Duration(secs) * time.Second
		}
		if d < 0 {
			d = 0
		}
		p.listen = min(d, httpMQTTListenMax)
	}
	_, p.retain = rr.Directives["retain"]
	p.clientID = strings.TrimSpace(rr.Directives["client-id"])
	if len(p.publish) > 0 && t.Topic == "" {
		return httpMQTTPlan{}, fmt.Errorf("mqtt: put the topic to publish to in the URL path (mqtt://host/topic)")
	}
	if len(p.publish) == 0 && len(p.subs) == 0 {
		if t.Topic == "" {
			return httpMQTTPlan{}, fmt.Errorf("mqtt: nothing to do: add a topic, a body or # @subscribe")
		}
		p.subs = []string{t.Topic}
	}
	return p, nil
}

// httpMQTTDoneMsg is a finished MQTT session.
type httpMQTTDoneMsg struct {
	label string
	text  string
	err   error
}

// httpMQTTSend sends the MQTT request req of file.
func (m *Model) httpMQTTSend(file string, vars []httpclient.Var, req httpclient.Request) tea.Cmd {
	s := m.httpS()
	if s.mqttRunning {
		return m.pushToastDetail(toast.Warn, "An MQTT session is still running", "It ends after its @listen time")
	}
	rv := httpResolver(file, vars, "")
	rr, un := rv.Resolve(req)
	if len(un) > 0 {
		return m.pushToastDetail(toast.Warn, "MQTT: unknown variable {{"+un[0].Name+"}}", "Define it in http-client.env.json or with @name = value")
	}
	plan, err := httpMQTTPlanFor(rr)
	if err != nil {
		return m.pushToastDetail(toast.Warn, err.Error(), "")
	}
	if plan.clientID == "" {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		plan.clientID = "termocode-" + hex.EncodeToString(b)
	}
	s.mqttRunning = true
	insecure := loadHTTPSettings().tlsInsecure()
	label := req.Label()
	secrets := append([]string(nil), rr.Secrets...)
	if plan.target.Password != "" {
		secrets = append(secrets, plan.target.Password)
	}
	run := func() tea.Msg {
		text, err := httpMQTTSession(context.Background(), plan, insecure, time.Now)
		for _, v := range secrets {
			if v != "" {
				text = strings.ReplaceAll(text, v, httpclient.Redacted)
			}
		}
		return httpMQTTDoneMsg{label: label, text: text, err: err}
	}
	toastCmd := m.pushToastDetail(toast.Info, "MQTT: connecting to "+plan.target.String(), fmt.Sprintf("listening %s", plan.listen))
	return tea.Batch(toastCmd, run)
}

// httpMQTTSession runs plan and returns its transcript. err is set when
// the session could not start; a drop while listening is in the text.
func httpMQTTSession(ctx context.Context, plan httpMQTTPlan, insecure bool, now func() time.Time) (string, error) {
	var b strings.Builder
	start := now()
	stamp := func() string { return fmt.Sprintf("%6.2fs", now().Sub(start).Seconds()) }
	fmt.Fprintf(&b, "MQTT %s  client %s\n\n", plan.target.String(), plan.clientID)
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	c, err := mqtt.DialTarget(dctx, plan.target, mqtt.Options{ClientID: plan.clientID, CleanSession: true, KeepAlive: 30 * time.Second}, insecure)
	cancel()
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Close() }()
	fmt.Fprintf(&b, "%s  connected\n", stamp())
	for _, f := range plan.subs {
		sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		q, err := c.Subscribe(sctx, f)
		cancel()
		if err != nil {
			fmt.Fprintf(&b, "%s  subscribe %s failed: %v\n", stamp(), f, err)
			continue
		}
		fmt.Fprintf(&b, "%s  subscribed %s (QoS %d)\n", stamp(), f, q)
	}
	for _, msg := range plan.publish {
		if err := c.Publish(plan.target.Topic, []byte(msg), plan.retain); err != nil {
			fmt.Fprintf(&b, "%s  publish failed: %v\n", stamp(), err)
			continue
		}
		fmt.Fprintf(&b, "%s  → %s\n%s\n", stamp(), plan.target.Topic, httpMQTTIndent(msg))
	}
	n := 0
	timer := time.NewTimer(plan.listen)
	defer timer.Stop()
listen:
	for {
		select {
		case msg, ok := <-c.Messages():
			if !ok {
				if err := c.Err(); err != nil {
					fmt.Fprintf(&b, "%s  connection ended: %v\n", stamp(), err)
				}
				break listen
			}
			n++
			flag := ""
			if msg.Retain {
				flag = " (retained)"
			}
			payload := string(msg.Payload)
			if len(payload) > httpMQTTPayloadMax {
				payload = payload[:httpMQTTPayloadMax] + fmt.Sprintf(" … (%d bytes)", len(msg.Payload))
			}
			fmt.Fprintf(&b, "%s  ← %s%s\n%s\n", stamp(), msg.Topic, flag, httpMQTTIndent(payload))
		case <-timer.C:
			break listen
		case <-ctx.Done():
			break listen
		}
	}
	fmt.Fprintf(&b, "\n%d message(s) received in %s.\n", n, plan.listen)
	return b.String(), nil
}

func httpMQTTIndent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return strings.Join(lines, "\n")
}

// onHTTPMQTTDone shows the transcript.
func (m *Model) onHTTPMQTTDone(msg httpMQTTDoneMsg) tea.Cmd {
	m.httpS().mqttRunning = false
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, "MQTT: could not connect", msg.err.Error())
	}
	m.ensureEditorWindowCurrent()
	if err := m.openReadOnlyPage("MQTT: "+msg.label, msg.text, "", "text"); err != nil {
		return m.pushToastDetail(toast.Errr, "Could not open the MQTT page", err.Error())
	}
	m.focus = FocusEditor
	return nil
}
