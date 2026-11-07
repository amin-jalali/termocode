package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Saved views from the phone (MOBUX-18, mobile plan 3.11.7): "Save as
// view" and "📌 Pin to Home". The laptop keeps them in the user's config
// folder (views.json, 0600), shared by every paired phone (owner
// 2026-11-07): specs and names only, never rows, so every open runs the
// spec again, live, through view.run. Load drops an entry that is no
// longer valid instead of running it. Every change is sent to every
// browser as ev views_saved.

const savedViewsFile = "views.json"

func init() {
	webOps[webproto.OpViewList] = webOpViewList
	webOps[webproto.OpViewSave] = webOpViewSave
	webOps[webproto.OpViewRename] = webOpViewRename
	webOps[webproto.OpViewDelete] = webOpViewDelete
	webOps[webproto.OpViewPin] = webOpViewPin
}

// savedViewSpecCheck checks a spec against the view registry before it
// is saved (unknown view, field, param or layout is refused). The shape
// was checked when the args decoded; the registry's full check
// (MOBUX-15, internal/views) plugs in here.
var savedViewSpecCheck = func(s *webproto.ViewSpec) error { return s.Validate() }

type savedViewsDoc struct {
	Version int                  `json:"version"`
	Views   []webproto.SavedView `json:"views"`
}

func savedViewsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, savedViewsFile), nil
}

// loadSavedViews reads views.json. A missing file is no views; malformed
// JSON is an error (so a save never wipes it); a broken, doubled or
// extra entry is dropped and pins are numbered 1..n again.
func loadSavedViews() ([]webproto.SavedView, error) {
	path, err := savedViewsPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc savedViewsDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	ids, names := map[string]bool{}, map[string]bool{}
	var out []webproto.SavedView
	for _, v := range doc.Views {
		v := v
		key := strings.ToLower(v.Name)
		if !webproto.ValidSavedView(&v) || ids[v.ID] || names[key] || len(out) == webproto.MaxSavedViews {
			continue
		}
		ids[v.ID], names[key] = true, true
		out = append(out, v)
	}
	sortSavedViews(out)
	renumberPins(out)
	return out, nil
}

func saveSavedViews(vs []webproto.SavedView) error {
	path, err := savedViewsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(savedViewsDoc{Version: 1, Views: vs}, "", "  ")
	if err != nil {
		return err
	}
	return importAtomicWrite(path, append(b, '\n'), 0o600)
}

// sortSavedViews puts pinned views first in their order, then the others
// by name (any case).
func sortSavedViews(vs []webproto.SavedView) {
	sort.SliceStable(vs, func(i, j int) bool {
		a, b := vs[i], vs[j]
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		if a.Pinned {
			return a.Order < b.Order
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

// renumberPins gives the pinned views (sorted first) order 1..n; at most
// MaxPinnedViews stay pinned.
func renumberPins(vs []webproto.SavedView) {
	sortSavedViews(vs)
	n := 0
	for i := range vs {
		if !vs[i].Pinned {
			vs[i].Order = 0
			continue
		}
		n++
		if n > webproto.MaxPinnedViews {
			vs[i].Pinned, vs[i].Order = false, 0
			continue
		}
		vs[i].Order = n
	}
	sortSavedViews(vs)
}

func pinnedCount(vs []webproto.SavedView) int {
	n := 0
	for _, v := range vs {
		if v.Pinned {
			n++
		}
	}
	return n
}

func savedViewIndex(vs []webproto.SavedView, id string) int {
	for i, v := range vs {
		if v.ID == id {
			return i
		}
	}
	return -1
}

func savedViewByName(vs []webproto.SavedView, name string) int {
	for i, v := range vs {
		if strings.EqualFold(v.Name, name) {
			return i
		}
	}
	return -1
}

func newSavedViewID(vs []webproto.SavedView) (string, error) {
	for range 8 {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		id := "v" + hex.EncodeToString(b[:])
		if savedViewIndex(vs, id) < 0 {
			return id, nil
		}
	}
	return "", errors.New("no free view id")
}

// pinAt pins the view id at place order (1 = first) among the pinned
// views; 0 keeps its place, or puts a newly pinned view last. The
// caller checked the pin cap.
func pinAt(vs []webproto.SavedView, id string, order int) {
	sortSavedViews(vs)
	var pins []string
	pos := -1
	for _, v := range vs {
		if !v.Pinned {
			continue
		}
		if v.ID == id {
			pos = len(pins)
			continue
		}
		pins = append(pins, v.ID)
	}
	switch {
	case order > 0:
		pos = min(order-1, len(pins))
	case pos < 0:
		pos = len(pins)
	}
	pins = append(pins[:pos], append([]string{id}, pins[pos:]...)...)
	place := map[string]int{}
	for k, p := range pins {
		place[p] = k + 1
	}
	for k := range vs {
		if n, ok := place[vs[k].ID]; ok {
			vs[k].Pinned, vs[k].Order = true, n
		}
	}
	renumberPins(vs)
}

func savedViewList(vs []webproto.SavedView) webproto.SavedViewList {
	if vs == nil {
		vs = []webproto.SavedView{}
	}
	return webproto.SavedViewList{Items: vs, Max: webproto.MaxSavedViews, MaxPinned: webproto.MaxPinnedViews}
}

// savedViewsChanged writes the list and tells every browser (Update only).
func savedViewsChanged(call *webCall, vs []webproto.SavedView, what string) bool {
	if err := saveSavedViews(vs); err != nil {
		recordError("[web] views.json: " + err.Error())
		call.fail(webproto.CodeInternal, "The view was not "+what+".")
		return false
	}
	h := call.conn.hub
	if h == nil || len(h.live) == 0 {
		return true
	}
	data, err := webData(savedViewList(vs))
	if err != nil {
		recordError("[web] views_saved: " + err.Error())
		return true
	}
	for c := range h.live {
		c.send(&webproto.Event{T: webproto.KindEv, Kind: webproto.EventViewsSaved, Data: data})
	}
	return true
}

func loadSavedViewsFor(call *webCall) ([]webproto.SavedView, bool) {
	vs, err := loadSavedViews()
	if err != nil {
		call.fail(webproto.CodeInternal, "views.json on the laptop is not valid JSON.")
		return nil, false
	}
	return vs, true
}

func webOpViewList(m *Model, call *webCall) tea.Cmd {
	if vs, ok := loadSavedViewsFor(call); ok {
		call.reply(savedViewList(vs))
	}
	return nil
}

func webOpViewSave(m *Model, call *webCall) tea.Cmd {
	a := call.args.(*webproto.ViewSaveArgs)
	if err := savedViewSpecCheck(&a.Spec); err != nil {
		call.fail(webproto.CodeBadRequest, "This view cannot be saved: "+err.Error())
		return nil
	}
	vs, ok := loadSavedViewsFor(call)
	if !ok {
		return nil
	}
	i := savedViewByName(vs, a.Name)
	replaced := i >= 0
	if !replaced && len(vs) >= webproto.MaxSavedViews {
		call.fail(webproto.CodeLimit, "32 views are saved already. Delete one first.")
		return nil
	}
	if a.Pin && (!replaced || !vs[i].Pinned) && pinnedCount(vs) >= webproto.MaxPinnedViews {
		call.fail(webproto.CodeLimit, "8 views are pinned to Home already. Unpin one first.")
		return nil
	}
	now := webNow().UnixMilli()
	if replaced {
		vs[i].Name, vs[i].Spec, vs[i].Saved, vs[i].By = a.Name, a.Spec, now, webAuditDevice(call.conn.device)
	} else {
		id, err := newSavedViewID(vs)
		if err != nil {
			call.fail(webproto.CodeInternal, "The view was not saved.")
			return nil
		}
		vs = append(vs, webproto.SavedView{ID: id, Name: a.Name, Spec: a.Spec, Saved: now, By: webAuditDevice(call.conn.device)})
		i = len(vs) - 1
	}
	id := vs[i].ID
	if a.Pin {
		pinAt(vs, id, 0)
	} else {
		sortSavedViews(vs)
	}
	if savedViewsChanged(call, vs, "saved") {
		call.reply(webproto.ViewSaved{ID: id, Replaced: replaced})
	}
	return nil
}

func webOpViewRename(m *Model, call *webCall) tea.Cmd {
	a := call.args.(*webproto.ViewRenameArgs)
	vs, ok := loadSavedViewsFor(call)
	if !ok {
		return nil
	}
	i := savedViewIndex(vs, a.ID)
	if i < 0 {
		call.fail(webproto.CodeNotFound, "That view is not there any more.")
		return nil
	}
	if j := savedViewByName(vs, a.Name); j >= 0 && j != i {
		call.fail(webproto.CodeConflict, "Another view has that name.")
		return nil
	}
	vs[i].Name, vs[i].Saved = a.Name, webNow().UnixMilli()
	sortSavedViews(vs)
	if savedViewsChanged(call, vs, "renamed") {
		call.reply(savedViewList(vs))
	}
	return nil
}

func webOpViewDelete(m *Model, call *webCall) tea.Cmd {
	a := call.args.(*webproto.ViewRefArgs)
	vs, ok := loadSavedViewsFor(call)
	if !ok {
		return nil
	}
	i := savedViewIndex(vs, a.ID)
	if i < 0 {
		call.fail(webproto.CodeNotFound, "That view is not there any more.")
		return nil
	}
	vs = append(vs[:i], vs[i+1:]...)
	renumberPins(vs)
	if savedViewsChanged(call, vs, "deleted") {
		call.reply(savedViewList(vs))
	}
	return nil
}

func webOpViewPin(m *Model, call *webCall) tea.Cmd {
	a := call.args.(*webproto.ViewPinArgs)
	vs, ok := loadSavedViewsFor(call)
	if !ok {
		return nil
	}
	i := savedViewIndex(vs, a.ID)
	if i < 0 {
		call.fail(webproto.CodeNotFound, "That view is not there any more.")
		return nil
	}
	switch {
	case a.On && !vs[i].Pinned && pinnedCount(vs) >= webproto.MaxPinnedViews:
		call.fail(webproto.CodeLimit, "8 views are pinned to Home already. Unpin one first.")
		return nil
	case a.On:
		pinAt(vs, a.ID, a.Order)
	default:
		vs[i].Pinned, vs[i].Order = false, 0
		renumberPins(vs)
	}
	if savedViewsChanged(call, vs, "changed") {
		call.reply(savedViewList(vs))
	}
	return nil
}
