// Package anim holds the small spring animations of termocode's Go-drawn
// chrome (overlay open, toast slide, smooth list scrolling), built on
// charmbracelet/harmonica.
//
// Rules:
//   - Only Go-drawn chrome animates. The nvim editor grid never does.
//   - A single TickMsg drives every animation, and the app schedules the
//     next tick only while something is still moving: no idle CPU.
//   - With Enabled == false every Set snaps to its target, so turning
//     animations off costs nothing per keystroke.
package anim

import (
	"math"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/harmonica"
)

// FPS is the animation frame rate.
const FPS = 60

// Enabled is the global switch. The app sets it from the "animations"
// setting (see Resolve).
var Enabled = false

// TickMsg advances every running animation by one frame.
type TickMsg time.Time

// Tick schedules the next animation frame.
func Tick() tea.Cmd {
	return tea.Tick(time.Second/FPS, func(t time.Time) tea.Msg { return TickMsg(t) })
}

// Spring tunings. Critically damped (no overshoot) springs read as calm,
// "native" motion; the frequency sets how fast they settle.
var (
	// Fast settles in ~150 ms: overlays and toasts.
	Fast = harmonica.NewSpring(harmonica.FPS(FPS), 32, 1.0)
	// Scroll settles in ~180 ms: list scrolling.
	Scroll = harmonica.NewSpring(harmonica.FPS(FPS), 26, 1.0)
)

// Value is a spring-animated number.
type Value struct {
	Pos, Vel, Target float64
	scroll           bool // use the Scroll tuning
}

// NewScroll returns a Value tuned for list scrolling.
func NewScroll() Value { return Value{scroll: true} }

// Set moves the target. When animations are off it jumps immediately.
func (v *Value) Set(target float64) {
	v.Target = target
	if !Enabled {
		v.Pos, v.Vel = target, 0
	}
}

// Jump moves position and target at once (no animation).
func (v *Value) Jump(x float64) { v.Pos, v.Vel, v.Target = x, 0, x }

// Moving reports whether the value has not reached its target yet.
func (v Value) Moving() bool { return v.Pos != v.Target }

// Step advances one frame and reports whether the value is still moving.
func (v *Value) Step() bool {
	if !v.Moving() {
		return false
	}
	sp := Fast
	if v.scroll {
		sp = Scroll
	}
	v.Pos, v.Vel = sp.Update(v.Pos, v.Vel, v.Target)
	// Snap once close: springs approach their target asymptotically.
	eps := 0.004
	if v.scroll {
		eps = 0.02
	}
	if math.Abs(v.Pos-v.Target) < eps && math.Abs(v.Vel) < eps*10 {
		v.Pos, v.Vel = v.Target, 0
		return false
	}
	return true
}

// Int is the position rounded to a whole cell.
func (v Value) Int() int { return int(math.Round(v.Pos)) }

// Mode is the "animations" setting.
type Mode string

const (
	ModeOn   Mode = "on"
	ModeOff  Mode = "off"
	ModeAuto Mode = "auto"
)

// ParseMode reads a setting value; unknown values mean auto.
func ParseMode(s string) Mode {
	switch Mode(s) {
	case ModeOn, ModeOff:
		return Mode(s)
	}
	return ModeAuto
}

// Resolve decides whether animations run. auto = on, except over SSH
// (every frame costs bandwidth and latency) or when the terminal is not
// truecolor (fades band badly with 256 / 16 colours).
func Resolve(mode Mode, truecolor bool, getenv func(string) string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch mode {
	case ModeOn:
		return true
	case ModeOff:
		return false
	}
	if getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != "" {
		return false
	}
	return truecolor
}
