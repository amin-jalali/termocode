package webproto

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The inbox across projects (MOB-53, mobile plan 3.7). Each attached
// project's hub sends its own inbox topic; the door (the paired gateway)
// and the page merge them into one stream, so Home, the watch socket and
// push counts show one list. A hub never sets ws on its items: the door
// stamps every item with the key of the hub it came from, so a project
// cannot speak for another. Pure.

// InboxPart is one project's inbox. WS "" is the home project.
type InboxPart struct {
	WS   string
	Snap InboxSnap
}

// InboxWSLen and InboxMaxHubs mirror webproto.WSLen and webproto.MaxHubs
// of MOB-50 (the door), so this file stands without it.
const (
	InboxWSLen   = 16
	InboxMaxHubs = 4
)

// InboxWS reports whether ws is a workspace key: 16 lower-case hex
// digits (as webproto.ValidWS of MOB-50).
func InboxWS(ws string) bool {
	if len(ws) != InboxWSLen {
		return false
	}
	for i := 0; i < len(ws); i++ {
		if c := ws[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// MaxInboxProjectName caps a project name in a notification line.
const MaxInboxProjectName = 24

// inboxNamesShown is how many project names a line lists before "+N".
const inboxNamesShown = 3

// MergeInbox merges the parts into one inbox: each item carries its
// project's ws ("" becomes home), the same ws + id twice is one item (the
// newer copy), newest first (ties by ws, then id, so the part order never
// matters), at most MaxInbox items with More counting the rest. Counts
// add up every part's counts, less the copies and bad items left out. A
// part with a ws that is not a workspace key is left out whole; so is an
// item with no id or an unknown kind or state.
func MergeInbox(home string, parts []InboxPart) InboxSnap {
	out := InboxSnap{Items: []InboxItem{}}
	at := map[string]int{}
	for _, p := range parts {
		ws := p.WS
		if ws == "" {
			ws = home
		}
		if !InboxWS(ws) {
			continue
		}
		addCounts(&out.Counts, p.Snap.Counts)
		out.More += max(p.Snap.More, 0)
		for _, it := range p.Snap.Items {
			if !inboxItemOK(it) {
				out.Counts.Add2(it.State, -1)
				continue
			}
			it.WS = ws
			it.Can = append([]string(nil), it.Can...)
			k := ws + "\x00" + it.ID
			if i, ok := at[k]; ok {
				old := out.Items[i]
				if it.Since > old.Since {
					out.Items[i] = it
					out.Counts.Add2(old.State, -1)
				} else {
					out.Counts.Add2(it.State, -1)
				}
				continue
			}
			at[k] = len(out.Items)
			out.Items = append(out.Items, it)
		}
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		if a.Since != b.Since {
			return a.Since > b.Since
		}
		if a.WS != b.WS {
			return a.WS < b.WS
		}
		return a.ID < b.ID
	})
	if n := len(out.Items); n > MaxInbox {
		out.More += n - MaxInbox
		out.Items = out.Items[:MaxInbox:MaxInbox]
	}
	out.Counts.clamp()
	return out
}

func inboxItemOK(it InboxItem) bool {
	if it.ID == "" || len(it.ID) > 2*MaxInboxTitle {
		return false
	}
	switch it.Kind {
	case InboxCard, InboxRun, InboxLane, InboxWorkflow, InboxTask, InboxCI, InboxRace:
	default:
		return false
	}
	switch it.State {
	case InboxNeedsYou, InboxWorking, InboxFailed, InboxDone:
		return true
	}
	return false
}

func addCounts(c *InboxCounts, d InboxCounts) {
	c.NeedsYou += max(d.NeedsYou, 0)
	c.Working += max(d.Working, 0)
	c.Failed += max(d.Failed, 0)
	c.Done += max(d.Done, 0)
}

// Add2 adds n to the count of one state (n may be negative).
func (c *InboxCounts) Add2(state string, n int) {
	switch state {
	case InboxNeedsYou:
		c.NeedsYou += n
	case InboxWorking:
		c.Working += n
	case InboxFailed:
		c.Failed += n
	case InboxDone:
		c.Done += n
	}
}

// clamp keeps a count that a hub under-reported from going below zero.
func (c *InboxCounts) clamp() {
	c.NeedsYou, c.Working, c.Failed, c.Done = max(c.NeedsYou, 0), max(c.Working, 0), max(c.Failed, 0), max(c.Done, 0)
}

// InboxNeedsYouLine is the one line a notification and the watch socket
// show: "1 thing needs you · shop", "3 things need you · shop, api",
// "5 things need you · a, b, c +2". Projects are named newest first by
// names[ws]; one with no name is "a project". "" when nothing needs you.
func InboxNeedsYouLine(s InboxSnap, names map[string]string) string {
	n := max(s.Counts.NeedsYou, 0)
	var order []string
	seen := map[string]bool{}
	for _, it := range s.Items {
		if it.State != InboxNeedsYou || seen[it.WS] {
			continue
		}
		seen[it.WS] = true
		order = append(order, it.WS)
	}
	if n < len(order) {
		n = len(order)
	}
	if n == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(strconv.Itoa(n))
	if n == 1 {
		b.WriteString(" thing needs you")
	} else {
		b.WriteString(" things need you")
	}
	if len(order) == 0 {
		return b.String()
	}
	b.WriteString(" · ")
	for i, ws := range order {
		if i == inboxNamesShown {
			b.WriteString(" +" + strconv.Itoa(len(order)-inboxNamesShown))
			break
		}
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(InboxProjectName(names[ws]))
	}
	return b.String()
}

// InboxProjectName makes a project name safe for a notification: the last
// path element only, no control characters, at most MaxInboxProjectName
// runes; "a project" when nothing is left.
func InboxProjectName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	n := 0
	for _, r := range name {
		if r == utf8.RuneError || unicode.IsControl(r) || !unicode.IsPrint(r) && r != ' ' {
			continue
		}
		if n == MaxInboxProjectName {
			break
		}
		b.WriteRune(r)
		n++
	}
	if s := strings.TrimSpace(b.String()); s != "" {
		return s
	}
	return "a project"
}

// InboxNewNeedsYou lists the items of next that need the user and did not
// in prev (by ws + id), newest first: one local notification each.
func InboxNewNeedsYou(prev, next InboxSnap) []InboxItem {
	had := map[string]bool{}
	for _, it := range prev.Items {
		if it.State == InboxNeedsYou {
			had[it.WS+"\x00"+it.ID] = true
		}
	}
	var out []InboxItem
	for _, it := range next.Items {
		if it.State == InboxNeedsYou && !had[it.WS+"\x00"+it.ID] {
			out = append(out, it)
		}
	}
	return out
}

// InboxItemLink is where a tap on the item's notification goes: a card
// opens its sheet on Home ("#/home?card=<ref>&ws=<ws>"), anything else
// its item ("#/home?item=<id>&ws=<ws>"). No ws: the home project.
func InboxItemLink(it InboxItem) string {
	var b strings.Builder
	if it.Kind == InboxCard {
		b.WriteString("#/home?card=" + url.QueryEscape(it.Ref))
	} else {
		b.WriteString("#/home?item=" + url.QueryEscape(it.ID))
	}
	if it.WS != "" {
		b.WriteString("&ws=" + url.QueryEscape(it.WS))
	}
	return b.String()
}
