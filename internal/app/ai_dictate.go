package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/voice"
)

// Voice input (CMP-48): AI: Dictate into Chat… runs the local voice
// program named in voice.json, after a Cancel-first card, and puts the
// words it heard into the chat input at the caret. Nothing is sent: the
// user reads the text and presses Enter. The AI can never start it.
// The web page dictates with the browser's own speech input instead
// (Composer.tsx); the laptop's microphone is only used from the laptop.

const (
	confirmKindAIDictate      confirmKindEnum = 4848 // Cancel-first: listen on the microphone?
	confirmKindAIDictateSetup confirmKindEnum = 4849 // not set up: open voice.json
	confirmKindAIDictateRec   confirmKindEnum = 4850 // listening: Stop and use / Discard
)

// aiDictateSession is a running voice program (*voice.Session).
type aiDictateSession interface {
	Stop()
	Cancel()
	Wait() voice.Result
}

// aiDictateLoad reads voice.json; aiDictateStart starts the program.
// Tests replace both.
var (
	aiDictateLoad = func() (voice.Settings, error) {
		dir, err := configDir()
		if err != nil {
			return voice.Settings{}, err
		}
		return voice.Load(dir)
	}
	aiDictateStart = func(ctx context.Context, s voice.Settings) (aiDictateSession, error) {
		return voice.Start(ctx, s)
	}
)

// aiDictate is the running dictation.
type aiDictate struct {
	id      uint64
	sess    aiDictateSession
	seconds int
}

type aiDictateDoneMsg struct {
	id  uint64
	res voice.Result
}

// aiDictateSeq numbers dictations, so a late result of an old one is
// never used.
var aiDictateSeq uint64

// openAIDictate is AI: Dictate into Chat….
func (m *Model) openAIDictate() tea.Cmd {
	s := m.aiS()
	if s.dictate != nil {
		return m.showAIDictateRec()
	}
	set, err := aiDictateLoad()
	if err != nil {
		return m.aiToastErr("Voice input", err)
	}
	if !set.Configured() {
		m.confirm = confirm.New(aiSpark()+" Voice input is not set up",
			"termocode records nothing itself. Name a local speech-to-text program in "+voice.FileName+
				": it listens on the microphone and prints the words. They go into the chat input; you read them and press Enter to send.",
			[]confirm.Button{
				{ID: "close", Title: "Close", Style: confirm.StylePrimary},
				{ID: "config", Title: "Open " + voice.FileName},
			}).WithWideText()
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindAIDictateSetup
		return nil
	}
	m.confirm = confirm.Risky(aiSpark()+" Listen on the microphone?",
		"Your voice program runs on this computer and listens for up to "+strconv.Itoa(set.Seconds())+
			" seconds. The words go into the chat input; nothing is sent until you press Enter. The AI never starts this.",
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "record", Title: "Listen"},
	).WithCode(set.Show(), "from "+voice.FileName)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIDictate
	return nil
}

// answerAIDictate is a button of the mic card ("" = Esc).
func (m *Model) answerAIDictate(id string) tea.Cmd {
	if id != "record" {
		return nil
	}
	s := m.aiS()
	if s.dictate != nil {
		return m.showAIDictateRec()
	}
	set, err := aiDictateLoad()
	if err != nil {
		return m.aiToastErr("Voice input", err)
	}
	sess, err := aiDictateStart(context.Background(), set)
	if err != nil {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "Voice program did not start", err.Error())
		return c
	}
	aiDictateSeq++
	d := &aiDictate{id: aiDictateSeq, sess: sess, seconds: set.Seconds()}
	s.dictate = d
	m.showAIDictateRec()
	return func() tea.Msg { return aiDictateDoneMsg{id: d.id, res: d.sess.Wait()} }
}

// answerAIDictateSetup is a button of the not-set-up card.
func (m *Model) answerAIDictateSetup(id string) tea.Cmd {
	if id != "config" {
		return nil
	}
	dir, err := configDir()
	if err != nil {
		return m.aiToastErr("Voice input", err)
	}
	p := filepath.Join(dir, voice.FileName)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return m.aiToastErr("Voice input", err)
		}
		if err := os.WriteFile(p, []byte(voice.DefaultBody()), 0o644); err != nil {
			return m.aiToastErr("Voice input", err)
		}
	}
	m.openFileAtLine(p, 1)
	return m.aiInfo("Name your voice program in \"command\", save, then run AI: Dictate into Chat… again")
}

// showAIDictateRec is the listening card.
func (m *Model) showAIDictateRec() tea.Cmd {
	d := m.aiS().dictate
	if d == nil {
		return nil
	}
	m.confirm = confirm.New(aiSpark()+" Listening…",
		"Speak now. It stops by itself after "+strconv.Itoa(d.seconds)+" seconds.\n"+
			"Stop and use puts the words into the chat input. Discard (or Esc) throws them away.",
		[]confirm.Button{
			{ID: "stop", Title: "Stop and use", Style: confirm.StylePrimary},
			{ID: "discard", Title: "Discard"},
		})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIDictateRec
	return nil
}

// answerAIDictateRec is a button of the listening card ("" = Esc:
// discard).
func (m *Model) answerAIDictateRec(id string) tea.Cmd {
	d := m.aiS().dictate
	if d == nil {
		return nil
	}
	if id == "stop" {
		d.sess.Stop()
		return m.aiInfo("Turning speech into text…")
	}
	d.sess.Cancel()
	return nil
}

// onAIDictateDone puts the words into the chat input at the caret and
// shows the chat. A result of an old dictation is dropped.
func (m *Model) onAIDictateDone(msg aiDictateDoneMsg) tea.Cmd {
	s := m.aiS()
	if s.dictate == nil || s.dictate.id != msg.id {
		return nil
	}
	s.dictate = nil
	if m.confirmOpen && m.confirmKind == confirmKindAIDictateRec {
		m.confirmOpen = false // it stopped by itself: the card goes
	}
	r := msg.res
	switch {
	case r.Canceled:
		return m.aiInfo("Dictation discarded")
	case r.Err != nil:
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "Voice input failed", r.Err.Error())
		return c
	case r.Text == "":
		return m.aiInfo("No words heard")
	}
	add := aiDictateJoin(s.input, s.caret, r.Text)
	m.aiInsert([]rune(add))
	c := m.openAIPanel(true)
	n := len(strings.Fields(r.Text))
	word := " words"
	if n == 1 {
		word = " word"
	}
	return tea.Batch(c, m.aiInfo("Dictated "+strconv.Itoa(n)+word+": read it, then Enter sends"))
}

// aiDictateJoin is text with a space in front when the caret follows a
// word.
func aiDictateJoin(input []rune, caret int, text string) string {
	if caret > 0 && caret <= len(input) && !unicode.IsSpace(input[caret-1]) {
		return " " + text
	}
	return text
}
