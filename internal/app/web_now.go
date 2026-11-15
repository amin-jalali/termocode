package app

import (
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/now"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Now (whose turn, why, which rule) on the inbox topic. Each source in
// webInboxItems says whose turn its item is and why, from its own state
// (an agent waits for an OK, CI is red on the branch, a task failed);
// nowPlace then drops what the user put away (Done, Later, Mute in the
// per-user store), places the rest by the user's rules (top rule wins,
// P1..P4) and counts each rule's items. now.act and now.rules change the
// store; they never act on a source.

func init() {
	webOps[webproto.OpNowAct] = (*Model).webNowAct
	webOps[webproto.OpNowRules] = (*Model).webNowRules
}

// nowStore is the user's Now store (now.json in the config folder),
// opened on first use.
func (m *Model) nowStore() *now.Store {
	if m.nowS == nil {
		path := ""
		if dir, err := configDir(); err == nil {
			path = filepath.Join(dir, "now.json")
		}
		m.nowS = now.Open(path)
	}
	return m.nowS
}

// nowTurn sets an item's turn, signal and reason.
func nowTurn(it *webproto.InboxItem, turn, signal, why string) {
	it.Turn, it.Signal, it.Why = turn, signal, why
	if signal == now.SigAgent || it.Kind == webproto.InboxRun || it.Kind == webproto.InboxLane || it.Kind == webproto.InboxWorkflow || it.Kind == webproto.InboxCard {
		it.Source = "agents"
	}
}

// nowAgentWaits: an agent waits for the user's OK.
func nowAgentWaits(it *webproto.InboxItem) {
	nowTurn(it, now.TurnMe, now.SigAgent, "Waits for your OK")
}

// nowByState sets the turn from the inbox state: needs you is an agent
// waiting, failed is the user's move (failed: its reason), working and
// done are nobody's.
func nowByState(it *webproto.InboxItem, failed string) {
	switch it.State {
	case webproto.InboxNeedsYou:
		nowAgentWaits(it)
	case webproto.InboxFailed:
		if failed == "" {
			failed = "Failed"
		}
		nowTurn(it, now.TurnMe, "", failed)
	case webproto.InboxWorking:
		nowTurn(it, now.TurnNone, "", "Running")
	default:
		nowTurn(it, now.TurnNone, "", "Done")
	}
}

// nowCI: a red CI on the branch is the user's move, with the fix; a
// pending one runs, a green one is nobody's.
func nowCI(it *webproto.InboxItem) {
	it.Source = "ci"
	switch it.State {
	case webproto.InboxFailed:
		it.Turn, it.Signal, it.Why = now.TurnMe, now.SigCIRed, "Your branch is red"
		it.Can = append(it.Can, "fix")
	case webproto.InboxWorking:
		it.Turn, it.Why = now.TurnNone, "Checks running"
	default:
		it.Turn, it.Why = now.TurnNone, "Green"
	}
}

// nowItem is the model's view of an inbox item.
func nowItem(it webproto.InboxItem) now.Item {
	x := now.Item{ID: it.ID, Kind: it.Kind, Source: it.Source, Title: it.Title, Turn: it.Turn, Waiting: it.Waiting,
		Signal: it.Signal, Head: it.Head, Why: it.Why}
	if it.Since > 0 {
		x.Since = time.UnixMilli(it.Since)
	}
	if it.WhyAt > 0 {
		x.WhyAt = time.UnixMilli(it.WhyAt)
	}
	if it.Due > 0 {
		x.Due = time.UnixMilli(it.Due)
	}
	if x.Turn == "" {
		x.Turn = now.TurnNone
	}
	return x
}

// nowPlace drops the items the user put away, places the rest by the
// user's rules and says how (the snap's Now info).
func (m *Model) nowPlace(items []webproto.InboxItem) ([]webproto.InboxItem, *webproto.NowInfo) {
	st := m.nowStore()
	at := webNow()
	prefs := st.Prefs()
	byID := make(map[string]webproto.InboxItem, len(items))
	xs := make([]now.Item, 0, len(items))
	for _, it := range items {
		byID[it.ID] = it
		xs = append(xs, nowItem(it))
	}
	kept := st.Apply(xs, at)
	count := map[string]int{}
	out := make([]webproto.InboxItem, 0, len(kept))
	for _, x := range kept {
		now.Place(&x, prefs)
		it := byID[x.ID]
		it.Turn, it.Why, it.Rule, it.Level = x.Turn, x.Why, x.Rule, x.Level
		if !x.WhyAt.IsZero() {
			it.WhyAt = x.WhyAt.UnixMilli()
		}
		if x.Rule != "" {
			count[x.Rule]++
		}
		out = append(out, it)
	}
	s := st.State()
	info := &webproto.NowInfo{Setup: s.Setup, Show: s.Answers.ShowN(), Top: s.Answers.Top, From: s.Answers.From, HideBots: s.Answers.HideBots,
		Rules: make([]webproto.NowRule, 0, len(s.Rules)), Hidden: len(items) - len(out)}
	for _, r := range s.Rules {
		info.Rules = append(info.Rules, webproto.NowRule{ID: r.ID, Name: r.Name, Sentence: r.Sentence, Signal: r.Signal, Level: r.Level, On: r.On, Count: count[r.ID]})
	}
	return out, info
}

// nowFind is the inbox item id before anything is put away.
func (m *Model) nowFind(id string) (now.Item, bool) {
	root, _ := m.rootDir()
	for _, it := range m.webInboxItems(webInboxPaths{root: root}) {
		if it.ID == id {
			return nowItem(it), true
		}
	}
	return now.Item{}, false
}

// webNowAct is now.act: done, later, mute or undo; the inbox shows it.
func (m *Model) webNowAct(call *webCall) tea.Cmd {
	a := call.args.(*webproto.NowActArgs)
	st := m.nowStore()
	at := webNow()
	if a.Do == "undo" {
		key, ok := st.Undo()
		if !ok {
			call.fail(webproto.CodeConflict, "Nothing to undo.")
			return nil
		}
		call.reply(webproto.NowActResult{ID: key})
		return nil
	}
	it, ok := m.nowFind(a.ID)
	if !ok {
		call.fail(webproto.CodeNotFound, "That item is gone.")
		return nil
	}
	var err error
	res := webproto.NowActResult{ID: it.ID}
	switch a.Do {
	case "done":
		err = st.Done(now.DoneKey(it), at)
	case "later":
		until, perr := now.ParseLater(a.When, at)
		if perr != nil {
			call.fail(webproto.CodeBadRequest, perr.Error())
			return nil
		}
		err, res.Until = st.Later(it.ID, until), until.UnixMilli()
	case "mute":
		err = st.Mute(now.MuteKey(it, a.Repo), at)
	}
	if err != nil {
		call.fail(webproto.CodeInternal, "Could not save: "+webName(err.Error()))
		return nil
	}
	call.reply(res)
	return nil
}

// webNowRules is now.rules: the first-run answers, a skip, or new rule
// rows; the inbox places the items again.
func (m *Model) webNowRules(call *webCall) tea.Cmd {
	a := call.args.(*webproto.NowRulesArgs)
	st := m.nowStore()
	var err error
	switch {
	case a.Setup != nil:
		err = st.Setup(now.Answers{Top: a.Setup.Top, From: a.Setup.From, Show: a.Setup.Show, HideBots: a.Setup.HideBots})
	case a.Skip:
		err = st.Setup(now.DefaultAnswers())
	default:
		rules := make([]now.Rule, 0, len(a.Rules))
		for _, r := range a.Rules {
			rules = append(rules, now.Rule{ID: r.ID, Name: r.Name, Sentence: r.Sentence, Signal: r.Signal, Level: r.Level, On: r.On})
		}
		err = st.SetRules(rules)
		if err != nil {
			call.fail(webproto.CodeBadRequest, err.Error())
			return nil
		}
	}
	if err != nil {
		call.fail(webproto.CodeInternal, "Could not save: "+webName(err.Error()))
		return nil
	}
	_, info := m.nowPlace(nil)
	call.reply(*info)
	return nil
}
