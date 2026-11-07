package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Pinned tracker blocks on the phone's Home (MOB-13; ai_tracker_pins.go):
// the list, a re-run on the laptop (off the UI thread: it may start the
// server) and unpin (the phone asks Cancel first). Pinning itself is the
// AI's tracker_pin tool, behind its laptop card.

func init() {
	webOps[webproto.OpAITrackerPins] = (*Model).webAITrackerPins
	webOps[webproto.OpAITrackerRun] = (*Model).webAITrackerRun
	webOps[webproto.OpAITrackerUnpin] = (*Model).webAITrackerUnpin
}

// webTrackerList is the stored pins as the phone sees them.
func (m *Model) webTrackerList() (webproto.TrackerPins, error) {
	trackerMu.Lock()
	pins, err := trackerLoad()
	trackerMu.Unlock()
	out := webproto.TrackerPins{Pins: []webproto.TrackerPin{}}
	if err != nil {
		return out, err
	}
	mgr, prof := m.aiMCP(), m.mcpActiveProfile()
	for _, p := range pins {
		out.Pins = append(out.Pins, trackerView(mgr, prof, p))
	}
	return out, nil
}

func (m *Model) webAITrackerPins(call *webCall) tea.Cmd {
	out, err := m.webTrackerList()
	if err != nil {
		recordError("[web] tracker pins: " + err.Error())
		call.fail(webproto.CodeInternal, "The pins could not be read on the laptop.")
		return nil
	}
	call.reply(out)
	return nil
}

// trackerFind is the stored pin with id.
func trackerFind(id string) (trackerPin, bool, error) {
	trackerMu.Lock()
	defer trackerMu.Unlock()
	pins, err := trackerLoad()
	for _, p := range pins {
		if p.ID == id {
			return p, true, err
		}
	}
	return trackerPin{}, false, err
}

func (m *Model) webAITrackerRun(call *webCall) tea.Cmd {
	a := call.args.(*webproto.TrackerIDArgs)
	p, ok, err := trackerFind(a.ID)
	switch {
	case err != nil:
		recordError("[web] tracker run: " + err.Error())
		call.fail(webproto.CodeInternal, "The pins could not be read on the laptop.")
		return nil
	case !ok:
		call.fail(webproto.CodeNotFound, "This pin is gone. It was unpinned.")
		return nil
	}
	mgr, prof := m.aiMCP(), m.mcpActiveProfile()
	call.later()
	return func() tea.Msg {
		call.reply(trackerRun(call.ctx, mgr, prof, p))
		return nil
	}
}

func (m *Model) webAITrackerUnpin(call *webCall) tea.Cmd {
	a := call.args.(*webproto.TrackerIDArgs)
	trackerMu.Lock()
	pins, err := trackerLoad()
	found := false
	if err == nil {
		kept := pins[:0]
		for _, p := range pins {
			if p.ID == a.ID {
				found = true
				continue
			}
			kept = append(kept, p)
		}
		if found {
			err = trackerSave(kept)
		}
	}
	trackerMu.Unlock()
	switch {
	case err != nil:
		recordError("[web] tracker unpin: " + err.Error())
		call.fail(webproto.CodeInternal, "The pin could not be removed on the laptop.")
		return nil
	case !found:
		call.fail(webproto.CodeNotFound, "This pin is gone already.")
		return nil
	}
	out, err := m.webTrackerList()
	if err != nil {
		call.fail(webproto.CodeInternal, "The pins could not be read on the laptop.")
		return nil
	}
	call.reply(out)
	return nil
}
