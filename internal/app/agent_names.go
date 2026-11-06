package app

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── AI-drafted worktree and branch names (AGX-16) ───────────────────────
//
// Worktree: Suggest a Name… asks for the task or issue title, sends only
// that (capped, secrets hidden) to the quick model slot, and lists up to
// 3 slugs with the cost; the user picks one (Enter copies it). Without a
// model nothing is sent: the row says to set a quick model.
// agentNamesSuggest is the same flow for the worktree dialog and the
// race card: onPick gets the chosen slug.

const (
	pickerKindAgentNames pickerKindEnum = 3748
	promptKindAgentNames promptKindEnum = 3749
	agentNamesPaletteID                 = "worktree-suggest-name"
	agentNamePfx                        = "name:"
	agentNameTaskMax                    = 300 // runes of the title sent
	agentNameSlugMax                    = 40
	agentNameTimeout                    = 30 * time.Second
	agentNamesSystem                    = "Suggest 3 short names for a git branch and worktree folder for the task below. " +
		"Each is a kebab-case slug of 2 to 5 words, only a-z, 0-9 and -. Reply with one slug per line and nothing else."
)

// agentNameRequest is the quick model request for task.
func agentNameRequest(model, task string) ai.Request {
	task = strings.Join(strings.Fields(ai.Redact(ai.RedactSecrets(task))), " ")
	if r := []rune(task); len(r) > agentNameTaskMax {
		task = string(r[:agentNameTaskMax])
	}
	return ai.Request{Model: model, Messages: []ai.Message{
		{Role: ai.RoleSystem, Content: agentNamesSystem},
		{Role: ai.RoleUser, Content: "Task: " + task},
	}}
}

// agentNameSlugs cleans the reply into at most 3 slugs.
func agentNameSlugs(reply string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(reply, "\n") {
		var b strings.Builder
		dash := false
		for _, r := range strings.ToLower(line) {
			switch {
			case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
				if dash && b.Len() > 0 {
					b.WriteByte('-')
				}
				b.WriteRune(r)
				dash = false
			default:
				dash = true
			}
		}
		s := b.String()
		// "1. fix-it": a leading list number is not part of the name.
		if i := strings.IndexByte(s, '-'); i > 0 && strings.Trim(s[:i], "0123456789") == "" && strings.TrimLeft(line, " ")[i] == '.' {
			s = s[i+1:]
		}
		if len(s) > agentNameSlugMax {
			s = strings.TrimRight(s[:agentNameSlugMax], "-")
		}
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
		if len(out) == 3 {
			break
		}
	}
	return out
}

// agentNamesPick is what Enter on a slug does (the palette: copy it).
type agentNamesPick func(m *Model, slug string) tea.Cmd

// openAgentNames is Worktree: Suggest a Name….
func (m *Model) openAgentNames() tea.Cmd {
	m.prompt = prompt.New(" Suggest a name ", "The task or issue title:", "")
	m.prompt.SetNote("Only this text goes to your quick model; secrets are hidden first.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindAgentNames
	m.agentNamesOnPick = nil
	return nil
}

// onAgentNamesTask sends the title to the quick model (nil and a "set a
// quick model" row when there is none).
func (m *Model) onAgentNamesTask(task string) tea.Cmd {
	return m.agentNamesSuggest(task, m.agentNamesOnPick)
}

// agentNamesSuggest asks for names for task; onPick runs on Enter (nil:
// copy the slug).
func (m *Model) agentNamesSuggest(task string, onPick agentNamesPick) tea.Cmd {
	task = strings.TrimSpace(task)
	if task == "" {
		return nil
	}
	m.agentNamesOnPick = onPick
	s := m.aiS()
	slot, err := s.resolveSlot(ai.SlotQuick)
	if err != nil {
		m.openAgentNamesPicker([]picker.Item{{ID: "none", Title: "No model: set a quick model", Hint: "AI: Edit Model Slots"}}, "Nothing was sent.")
		return nil
	}
	m.openAgentNamesPicker([]picker.Item{{ID: "none", Title: "Asking " + slot.cfg.Model + "…"}}, "")
	req := agentNameRequest(slot.cfg.Model, task)
	gen := m.agentNamesGen + 1
	m.agentNamesGen = gen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), agentNameTimeout)
		defer cancel()
		text, last := ai.Collect(slot.p.Stream(ctx, req))
		return applyMsg(func(m *Model) tea.Cmd { return m.onAgentNamesReply(gen, slot, text, last) })
	}
}

// onAgentNamesReply lists the slugs with the cost.
func (m *Model) onAgentNamesReply(gen int, slot aiSlot, text string, last ai.Chunk) tea.Cmd {
	if gen != m.agentNamesGen {
		return nil
	}
	s := m.aiS()
	cost := slot.cfg.Model + " · cost not known"
	if u := last.Usage; u != nil {
		c := *u
		e := aiUsageEntry{Agent: "names", Model: slot.cfg.Model, U: c, Sub: slot.cfg.Kind == ai.KindClaudeOAuth}
		if usd, ok := ai.NewPricing(s.settings.Prices).Cost(slot.cfg.Kind, c); ok {
			e.Cost = &usd
			cost = slot.cfg.Model + " · " + aiDollars(usd)
		} else if e.Sub {
			cost = slot.cfg.Model + " · subscription"
		}
		s.recordUsage(e)
	}
	if !m.pickerOpen || m.pickerKind != pickerKindAgentNames {
		return nil // closed while it was asking
	}
	if last.Err != nil {
		m.openAgentNamesPicker([]picker.Item{{ID: "none", Title: "No names: " + ai.Redact(last.Err.Error())}}, cost)
		return nil
	}
	var items []picker.Item
	for _, slug := range agentNameSlugs(text) {
		items = append(items, picker.Item{ID: agentNamePfx + slug, Title: slug, Hint: "branch " + slug})
	}
	if len(items) == 0 {
		items = []picker.Item{{ID: "none", Title: "No usable names in the reply"}}
	}
	m.openAgentNamesPicker(items, cost)
	return nil
}

// openAgentNamesPicker shows rows with a note.
func (m *Model) openAgentNamesPicker(items []picker.Item, note string) {
	m.picker = picker.NewItems(" Suggested names ", items)
	if note != "" {
		m.picker.SetNote([]string{note})
	}
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindAgentNames
}

// onAgentNamePicked runs the pick.
func (m *Model) onAgentNamePicked(id string) tea.Cmd {
	slug, ok := strings.CutPrefix(id, agentNamePfx)
	if !ok || len(agentNameSlugs(slug)) != 1 || agentNameSlugs(slug)[0] != slug {
		return nil
	}
	if f := m.agentNamesOnPick; f != nil {
		m.agentNamesOnPick = nil
		return f(m, slug)
	}
	m.writeClipboard(slug)
	return m.pushToast(toast.Info, "Copied "+slug)
}

func init() {
	aiActionPolicies[agentNamesPaletteID] = aiNever(aiNoPicker + "; the user picks the name")
}
