package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// homeModel is a laptop with an open chat and a main model slot that
// answers the phone's chat.
func homeModel(t *testing.T) (Model, *compactFake) {
	m := aiTestModel(t)
	s := m.ai
	s.provider = fakeAIProvider{reply: "main"}
	s.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	main := &compactFake{replies: []ai.Chunk{{Delta: "Lint failed in web_chat.go."}, {Delta: "Run make lint."}}}
	s.slotFake = map[string]aiSlot{ai.SlotMain: {p: main, cfg: ai.Config{Kind: ai.KindOllama, Model: "fake-1"}}}
	s.conv.Messages = []ai.StoredMessage{{Role: ai.StoredUser, Text: "refactor the errors"}, {Role: ai.StoredAssistant, Text: "working on errors.go"}}
	m.output = newOutputStore()
	return m, main
}

func runHomeWork(t *testing.T, work tea.Cmd) aiHomeDoneMsg {
	t.Helper()
	if work == nil {
		t.Fatal("no home request")
	}
	msg, ok := work().(aiHomeDoneMsg)
	if !ok {
		t.Fatal("not a home answer")
	}
	return msg
}

func homeMsgs(f map[string]any) []any {
	d, _ := f["data"].(map[string]any)
	ms, _ := d["messages"].([]any)
	return ms
}

// Spec (MOB-16, owner Q3): chat.home sends the phone's message to its own
// background chat, "Phone · <device>", in this workspace. The laptop's
// open chat and view stay as they are; the answer comes from the main
// model with no tools; the chat is stored (the laptop's history lists it)
// and a later send goes on with it. chat.home {} reads it back.
func TestWebChatHomeKeepsTheLaptopChatStill(t *testing.T) {
	m, main := homeModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	c.name = "Pixel 8"
	open := m.ai.conv.ID
	call, work := webTestCallLater(t, &m, c, webproto.OpChatHome, `{"text":"why did the build fail?"}`)
	if f := webTestAnswer(t, c, call); f != nil || work == nil {
		t.Fatalf("answered before the model: %v", f)
	}
	m.handleAIMsg(runHomeWork(t, work))
	f := webTestAnswer(t, c, call)
	if f == nil || f["t"] != "res" {
		t.Fatalf("answer: %v", f)
	}
	d := f["data"].(map[string]any)
	if d["title"] != "Phone · Pixel 8" || d["busy"] != false || len(homeMsgs(f)) != 2 {
		t.Fatalf("home chat: %v", d)
	}
	if m.ai.conv.ID != open || len(m.ai.conv.Messages) != 2 || m.confirmOpen {
		t.Fatalf("the laptop's chat moved: %s %+v", m.ai.conv.ID, m.ai.conv.Messages)
	}
	q := main.requests()
	if len(q) != 1 || len(q[0].Tools) != 0 || q[0].Messages[len(q[0].Messages)-1].Content != "why did the build fail?" {
		t.Fatalf("home request %+v", q)
	}
	id, _ := d["id"].(string)
	stored, err := m.ai.store.Load(id)
	if err != nil || stored.Title != "Phone · Pixel 8" || len(stored.Messages) != 2 || stored.Messages[1].Text != "Lint failed in web_chat.go." {
		t.Fatalf("stored home chat: %+v %v", stored, err)
	}

	// A second send goes on with the same chat.
	call, work = webTestCallLater(t, &m, c, webproto.OpChatHome, `{"text":"and the fix?"}`)
	m.handleAIMsg(runHomeWork(t, work))
	f = webTestAnswer(t, c, call)
	if f == nil || f["data"].(map[string]any)["id"] != id || len(homeMsgs(f)) != 4 {
		t.Fatalf("follow-up: %v", f)
	}
	if q = main.requests(); len(q) != 2 || len(q[1].Messages) != 4 || q[1].Messages[1].Content != "why did the build fail?" {
		t.Fatalf("the follow-up lost the home history: %+v", q)
	}

	// Reading it back costs no tokens.
	f, _ = webTestCall(t, &m, c, webproto.OpChatHome, `{}`)
	if f["t"] != "res" || f["data"].(map[string]any)["id"] != id || len(homeMsgs(f)) != 4 || len(main.requests()) != 2 {
		t.Fatalf("read: %v", f)
	}
	if a := webTestAudit(&m); strings.Contains(a, "build fail") {
		t.Errorf("the audit has the message text:\n%s", a)
	}
}

// Spec (MOB-16): one AI turn at a time per workspace. A phone send while
// the laptop's AI answers is busy; while the phone chat answers, a second
// phone send, a chat.send and a laptop send are refused; chat.stop stops
// the phone chat's turn too.
func TestWebChatHomeOneTurnAtATime(t *testing.T) {
	m, _ := homeModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	m.ai.streaming = true
	if f, _ := webTestCall(t, &m, c, webproto.OpChatHome, `{"text":"hi"}`); webTestErr(f) != "busy" {
		t.Fatalf("while the laptop answers: %v", f)
	}
	m.ai.streaming = false
	call, work := webTestCallLater(t, &m, c, webproto.OpChatHome, `{"text":"hi"}`)
	if work == nil || !m.ai.home.busy {
		t.Fatal("the phone chat is not answering")
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpChatHome, `{"text":"again"}`); webTestErr(f) != "busy" {
		t.Fatalf("second phone send: %v", f)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpChatSend, `{"text":"laptop chat"}`); webTestErr(f) != "busy" {
		t.Fatalf("chat.send while the phone chat answers: %v", f)
	}
	n := len(m.ai.conv.Messages)
	m.sendAIChat("typed on the laptop")
	if len(m.ai.conv.Messages) != n {
		t.Fatal("the laptop started a second turn")
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpChatStop, `{}`); f["t"] != "res" || m.ai.home.busy {
		t.Fatalf("stop: %v busy=%v", f, m.ai.home.busy)
	}
	m.handleAIMsg(runHomeWork(t, work)) // the stopped turn's answer is dropped
	if f := webTestAnswer(t, c, call); f == nil || f["t"] == "res" {
		t.Fatalf("a stopped turn answered: %v", f)
	}
}

// Spec (MOB-16): a phone chat the laptop has open is not sent to in the
// background (the laptop would save over it): the phone is told so.
func TestWebChatHomeOpenOnTheLaptop(t *testing.T) {
	m, _ := homeModel(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	_, work := webTestCallLater(t, &m, c, webproto.OpChatHome, `{"text":"hi"}`)
	msg := runHomeWork(t, work)
	m.handleAIMsg(msg)
	stored, err := m.ai.store.Load(msg.id)
	if err != nil {
		t.Fatal(err)
	}
	m.aiShowStoredChat(stored)
	if f, _ := webTestCall(t, &m, c, webproto.OpChatHome, `{"text":"more"}`); webTestErr(f) != "conflict" {
		t.Fatalf("open on the laptop: %v", f)
	}
}
