package app

import (
	"reflect"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The ai topic (WEBP-3): the laptop's AI as the chat header's profile
// and model picker and the editor's AI see it (no key, no endpoint).
// The snapshot is rebuilt when the AI's state moves (a cheap key on
// Update) and at most every webAIEvery otherwise (a project profile's
// trust is in the session file); it is sent only when it changed.
//
// The page may ask to use another profile or model (web design 3.3: the
// web cannot change them itself): a laptop card asks, Cancel first, and
// only the laptop's yes switches. A project profile the laptop has not
// trusted is never offered: trust is the laptop's decision alone.

const webAIEvery = 2 * time.Second

func init() {
	webTopics["ai"] = webTopicDef{snapshot: webAISnapshot, apply: webApplyReplace[webproto.AISnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishAI)
	webAskOps[webproto.OpAIUseProfile] = (*Model).webAIUseProfile
	webAskOps[webproto.OpAIUseModel] = (*Model).webAIUseModel
}

// webAIDelta is an ai delta: the whole snapshot.
type webAIDelta webproto.AISnap

func (webAIDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webAIDelta)
	return n, ok
}

// webAIPub is what the ai topic sent last (Update only).
type webAIPub struct {
	last  *webproto.AISnap
	key   webAIKeyState
	built time.Time
}

// webAIKeyState changes when the AI's setup, profile or toggles change.
type webAIKeyState struct {
	prov                            ai.Provider
	kind, model, profile            string
	profiles                        int
	inline, nextEdit, wt, configure bool
}

func (m *Model) webAIKey() webAIKeyState {
	s := m.aiS()
	return webAIKeyState{prov: s.provider, kind: s.cfg.Kind, model: s.cfg.Model, profile: s.activeProfile().Name,
		profiles: len(s.profiles.Names()), inline: s.settings.InlineEnabled(), nextEdit: s.settings.NextEditEnabled(),
		wt: s.settings.UseWorktrees(), configure: m.aiConfigured()}
}

func webAISnapshot(m *Model, _ string) (any, error) {
	snap := m.webAIBuild()
	if m.web != nil {
		p := &m.web.more.ai
		p.last, p.key, p.built = &snap, m.webAIKey(), webNow()
	}
	return snap, nil
}

// webPublishAI sends the snapshot when it changed.
func (m *Model) webPublishAI(tea.Msg) tea.Cmd {
	h := m.web
	p := &h.more.ai
	if !h.webListens("ai") {
		p.last = nil
		return nil
	}
	if p.last == nil {
		return nil
	}
	now, key := webNow(), m.webAIKey()
	if key == p.key && now.Sub(p.built) < webAIEvery {
		return nil
	}
	p.key, p.built = key, now
	cur := m.webAIBuild()
	if reflect.DeepEqual(*p.last, cur) {
		return nil
	}
	p.last = &cur
	h.publish("ai", webAIDelta(cur))
	return nil
}

// webAIKind is the provider kind the model list is for.
func (s *aiState) webAIKind() string {
	if s.cfg.Kind != "" {
		return s.cfg.Kind
	}
	if k := s.settings.Provider; k != "none" {
		return k
	}
	return ""
}

// webAIModels are the models the page may pick: the laptop's model
// picker's list for the provider, the model in use first when it is not
// on it.
func (s *aiState) webAIModels() []string {
	kind := s.webAIKind()
	if kind == "" {
		return []string{}
	}
	out := []string{}
	if s.cfg.Model != "" && !slices.Contains(ai.SuggestedModels(kind), s.cfg.Model) {
		out = append(out, s.cfg.Model)
	}
	for _, id := range ai.SuggestedModels(kind) {
		if len(out) == webproto.MaxAIModels {
			break
		}
		out = append(out, id)
	}
	return out
}

// webAIBuild is the ai topic's data.
func (m *Model) webAIBuild() webproto.AISnap {
	s := m.aiS()
	out := webproto.AISnap{Configured: m.aiConfigured(), Model: webName(s.cfg.Model), Models: []string{},
		Profile: webName(s.activeProfile().Name), Profiles: []webproto.AIProfile{},
		Inline: s.settings.InlineEnabled(), NextEdit: s.settings.NextEditEnabled(), Worktrees: s.settings.UseWorktrees()}
	if kind := s.webAIKind(); kind != "" {
		out.Provider = ai.KindLabel(kind)
	}
	for _, id := range s.webAIModels() {
		out.Models = append(out.Models, webName(id))
	}
	for _, n := range s.profiles.Names() {
		if len(out.Profiles) == webproto.MaxAIProfiles {
			break
		}
		p, _ := s.profiles.Get(n)
		src := p.Source
		if src != ai.ProfileSourceUser && src != ai.ProfileSourceProject {
			src = ai.ProfileSourceBuiltin
		}
		out.Profiles = append(out.Profiles, webproto.AIProfile{Name: webName(n), Source: src, Trust: m.aiProfileTrust(p), Hint: webName(s.aiPipelineText(p, 80))})
	}
	return out
}

// webAIUseProfile asks the laptop to switch the chat's agent profile.
func (m *Model) webAIUseProfile(call *webCall) *webAsk {
	name := call.args.(*webproto.AIUseArgs).Name
	s := m.aiS()
	p, ok := s.profiles.Get(name)
	switch {
	case !ok:
		call.fail(webproto.CodeNotFound, "There is no such profile.")
		return nil
	case m.aiProfileTrust(p) != aiTrustUse:
		call.fail(webproto.CodeForbidden, "Trust this project profile on the laptop first.")
		return nil
	case s.activeProfile().Name == name:
		call.fail(webproto.CodeConflict, "That profile is in use already.")
		return nil
	}
	return &webAsk{
		summary: name,
		title:   "Use profile " + name + "?",
		body:    "The AI chat uses profile " + name + " for the next messages.",
		action:  "Use",
		run:     func(m *Model) tea.Cmd { return m.selectAIProfile(name) },
	}
}

// webAIUseModel asks the laptop to use another model of its provider
// (one of the laptop's picker's models, never a typed id).
func (m *Model) webAIUseModel(call *webCall) *webAsk {
	name := call.args.(*webproto.AIUseArgs).Name
	s := m.aiS()
	switch {
	case !slices.Contains(s.webAIModels(), name):
		call.fail(webproto.CodeNotFound, "Pick one of the laptop's models.")
		return nil
	case s.cfg.Model == name:
		call.fail(webproto.CodeConflict, "That model is in use already.")
		return nil
	}
	return &webAsk{
		summary: name,
		title:   "Use model " + name + "?",
		body:    "The AI chat uses model " + name + " from now on.",
		action:  "Use",
		run:     func(m *Model) tea.Cmd { return m.saveAIModel(name) },
	}
}
