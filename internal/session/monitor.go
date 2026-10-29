package session

import "time"

// clockReading is one look at both clocks. The monotonic clock stops
// while the machine sleeps; the wall clock does not, so a gap between
// them means the machine just woke up.
type clockReading struct {
	mono time.Duration // since some fixed point
	wall time.Time
}

// wakeJump is how far the wall clock must run ahead of the monotonic
// clock between two ticks to count as a sleep.
const wakeJump = 3 * time.Second

// linkMonitor decides when `termocode ssh` pings the far side and when it
// gives the link up: every interval a ping; no byte at all for dead
// means the link is gone. After a sleep the old link is checked at once:
// a ping, and it is gone when nothing comes back within wakeWait.
type linkMonitor struct {
	interval, dead, wakeWait time.Duration

	lastRecv, lastPing, lastTick clockReading
	wakeDeadline                 time.Duration // 0: not checking a wake
}

func newLinkMonitor(interval, dead time.Duration, now clockReading) *linkMonitor {
	return &linkMonitor{interval: interval, dead: dead, wakeWait: min(dead, 5*time.Second),
		lastRecv: now, lastPing: now, lastTick: now}
}

// Received notes that bytes came from the far side.
func (m *linkMonitor) Received(now clockReading) {
	m.lastRecv = now
	m.wakeDeadline = 0
}

// Tick is called often (about every second, or after a wake). It says
// whether to send a ping now, whether the link is gone, and whether the
// machine woke up since the last tick.
func (m *linkMonitor) Tick(now clockReading) (ping, dead, woke bool) {
	jump := now.wall.Sub(m.lastTick.wall) - (now.mono - m.lastTick.mono)
	m.lastTick = now
	if jump > wakeJump {
		woke = true
		m.wakeDeadline = now.mono + m.wakeWait
		ping = true
	}
	if now.mono-m.lastPing.mono >= m.interval {
		ping = true
	}
	if ping {
		m.lastPing = now
	}
	dead = now.mono-m.lastRecv.mono >= m.dead ||
		(m.wakeDeadline > 0 && now.mono >= m.wakeDeadline)
	return ping, dead, woke
}

// realClock reads both clocks; start is the monotonic zero.
func realClock(start time.Time) clockReading {
	now := time.Now()
	return clockReading{mono: now.Sub(start), wall: now.Round(0)}
}
