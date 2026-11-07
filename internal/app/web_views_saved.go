package app

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Saved and pinned views from the phone (MOB-71): view.list, view.save,
// view.delete and view.pin. The saved views live in views.json (shared by
// every paired phone), each device's Home pins on its devices.json entry.
// A connection without a paired device (a one-time link) can list and
// save but has no Home of its own to pin to.

func init() {
	webOps[webproto.OpViewList] = (*Model).webViewList
	webOps[webproto.OpViewSave] = (*Model).webViewSave
	webOps[webproto.OpViewDelete] = (*Model).webViewDelete
	webOps[webproto.OpViewPin] = (*Model).webViewPin
}

// webViewStores are the saved views and the devices of this laptop.
func webViewStores() (*webdevices.ViewStore, *webdevices.Store, error) {
	dir, err := webDevicesDir()
	if err != nil {
		return nil, nil, err
	}
	return webdevices.OpenViews(dir), webdevices.Open(dir), nil
}

// webViewListFor is every saved view and the pins of device dev (pins
// whose view is gone are left out).
func webViewListFor(dev string) (webproto.ViewList, error) {
	vs, ds, err := webViewStores()
	if err != nil {
		return webproto.ViewList{}, err
	}
	views, err := vs.List()
	if err != nil {
		return webproto.ViewList{}, err
	}
	out := webproto.ViewList{Views: make([]webproto.SavedView, 0, len(views)), Pins: []string{}}
	have := map[string]bool{}
	for _, v := range views {
		have[v.ID] = true
		out.Views = append(out.Views, webproto.SavedView{ID: v.ID, Name: v.Name, Spec: v.Spec, Updated: v.Updated.UnixMilli()})
	}
	if dev != "" {
		if d, ok, err := ds.Get(dev); err == nil && ok {
			for _, p := range d.Pins {
				if have[p] {
					out.Pins = append(out.Pins, p)
				}
			}
		}
	}
	return out, nil
}

func (m *Model) webViewList(call *webCall) tea.Cmd {
	l, err := webViewListFor(call.conn.device)
	if err != nil {
		call.fail(webproto.CodeInternal, "Could not read the saved views.")
		return nil
	}
	call.reply(l)
	return nil
}

func (m *Model) webViewSave(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ViewSaveArgs)
	vs, ds, err := webViewStores()
	if err != nil {
		call.fail(webproto.CodeInternal, "No config folder.")
		return nil
	}
	v, err := vs.Save(webdevices.View{ID: a.ID, Name: a.Name, Spec: a.Spec})
	switch {
	case errors.Is(err, webdevices.ErrViewsFull):
		call.fail(webproto.CodeLimit, "32 views are saved. Delete one first.")
		return nil
	case errors.Is(err, webdevices.ErrNoView):
		call.fail(webproto.CodeNotFound, "That view is gone.")
		return nil
	case err != nil:
		call.fail(webproto.CodeInternal, "Could not save the view.")
		return nil
	}
	if a.Pin && call.conn.device != "" {
		if _, err := ds.SetPin(call.conn.device, v.ID, true, webproto.MaxViewPins); errors.Is(err, webdevices.ErrPinsFull) {
			m.webAuditLine(call.op, v.Name, call.conn.device, "saved, not pinned (full)")
			call.fail(webproto.CodeLimit, "Saved. 8 views are pinned here already: unpin one to pin it.")
			return nil
		}
	}
	m.webAuditLine(call.op, v.Name, call.conn.device, "ok")
	call.reply(webproto.SavedView{ID: v.ID, Name: v.Name, Spec: v.Spec, Updated: v.Updated.UnixMilli()})
	return nil
}

func (m *Model) webViewDelete(call *webCall) tea.Cmd {
	id := call.args.(*webproto.ViewIDArgs).ID
	vs, ds, err := webViewStores()
	if err != nil {
		call.fail(webproto.CodeInternal, "No config folder.")
		return nil
	}
	v, ok, _ := vs.Get(id)
	found, err := vs.Delete(id)
	if err != nil {
		call.fail(webproto.CodeInternal, "Could not delete the view.")
		return nil
	}
	if !found || !ok {
		call.fail(webproto.CodeNotFound, "That view is gone.")
		return nil
	}
	if err := ds.Unpin(id); err != nil {
		recordError("[web] view.delete: unpin: " + err.Error())
	}
	m.webAuditLine(call.op, v.Name, call.conn.device, "ok")
	call.reply(nil)
	return nil
}

func (m *Model) webViewPin(call *webCall) tea.Cmd {
	a := call.args.(*webproto.ViewPinArgs)
	dev := call.conn.device
	if dev == "" {
		call.fail(webproto.CodeForbidden, "Pair this phone to pin views to its Home.")
		return nil
	}
	vs, ds, err := webViewStores()
	if err != nil {
		call.fail(webproto.CodeInternal, "No config folder.")
		return nil
	}
	v, ok, err := vs.Get(a.ID)
	if err != nil || (!ok && a.On) {
		call.fail(webproto.CodeNotFound, "That view is gone.")
		return nil
	}
	found, err := ds.SetPin(dev, a.ID, a.On, a.Order)
	switch {
	case errors.Is(err, webdevices.ErrPinsFull):
		call.fail(webproto.CodeLimit, "8 views are pinned here. Unpin one first.")
		return nil
	case err != nil:
		call.fail(webproto.CodeInternal, "Could not change the pins.")
		return nil
	case !found:
		call.fail(webproto.CodeForbidden, "Pair this phone to pin views to its Home.")
		return nil
	}
	name := v.Name
	if name == "" {
		name = a.ID
	}
	m.webAuditLine(call.op, name, dev, "ok")
	l, err := webViewListFor(dev)
	if err != nil {
		call.fail(webproto.CodeInternal, "Could not read the saved views.")
		return nil
	}
	call.reply(l)
	return nil
}
