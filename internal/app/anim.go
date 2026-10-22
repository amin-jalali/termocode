package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/anim"
)

// Animation glue: overlays fade/slide in, toasts slide, lists scroll
// smoothly. One anim.TickMsg steps everything; the next tick is scheduled
// only while something still moves, so an idle editor costs no CPU. With
// animations off (anim.Enabled == false) every value snaps to its target
// and no tick is ever scheduled.

// overlayKind names the floating overlay that is open ("" = none). A change
// of kind (re)starts the open animation.
func (m Model) overlayKind() string {
	switch {
	case m.settingsModalOpen:
		return "settings"
	case m.pickerOpen:
		return "picker"
	case m.recentsOpen:
		return "recents"
	case m.promptOpen:
		return "prompt"
	case m.confirmOpen:
		return "confirm"
	case m.connectOpen:
		return "connect"
	case m.importOpen:
		return "import"
	case m.reviewOpen:
		return "review"
	case m.menuOpen:
		return "menu"
	case m.imageOpen:
		return "image"
	}
	return ""
}

// overlayOpts are the compositor options for the current overlay frame.
func (m Model) overlayOpts() ModalOpts {
	return ModalOpts{Animating: m.overlayAnim.Moving(), Progress: m.overlayAnim.Pos}
}

func (m Model) modalOpts() *ModalOpts {
	o := m.overlayOpts()
	return &o
}

// anyAnimating reports whether any animation still needs frames.
func (m Model) anyAnimating() bool {
	return m.overlayAnim.Moving() || m.toast.Animating() || m.picker.Animating() || m.explorer.Animating()
}

// stepAnimations advances every running animation by one frame.
func (m *Model) stepAnimations() {
	m.overlayAnim.Step()
	m.toast, _ = m.toast.Step()
	m.picker.AnimStep()
	m.explorer.AnimStep()
}

// afterUpdateAnim runs after every Update: it starts the open animation
// when an overlay appears and keeps the tick loop alive while needed.
func (m *Model) afterUpdateAnim(prevKind string) tea.Cmd {
	if !anim.Enabled {
		return nil
	}
	if k := m.overlayKind(); k != prevKind && k != "" {
		m.overlayAnim.Jump(0)
		m.overlayAnim.Set(1)
	}
	if !m.animTicking && m.anyAnimating() {
		m.animTicking = true
		return anim.Tick()
	}
	return nil
}
