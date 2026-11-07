package ai

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/workflow/schedule"
)

// Chat schedules (CMP-43). A chat can run again by itself at set times
// ("rerun this chat at 9:00"): at each slot the chat gets its saved
// message as a new user turn. The user sets it (AI: Schedule This
// Chat…) or the AI proposes it for its own chat (chat_schedule_set);
// both end in a card. The schedule lives in the chat's own file.

// Who set a chat schedule.
const (
	ChatSchedByUser = "user"
	ChatSchedByAI   = "ai"
)

const (
	// ChatSchedPromptMax caps the saved message.
	ChatSchedPromptMax = 4000
	// ChatSchedMinGap is the shortest time between two runs: each run is
	// a paid turn.
	ChatSchedMinGap = time.Hour
	// ChatSchedMax caps how many chats may have a schedule.
	ChatSchedMax = 20
)

// ChatSchedule is when a chat runs again and what it sends.
type ChatSchedule struct {
	// When is the schedule as written: "at 09:00", "daily 09:00",
	// "weekdays 09:00", "weekly mon 09:00", "monthly 1 09:00",
	// "hourly :15" or 5 cron fields.
	When string `json:"when"`
	// TZ is an IANA zone; "" is the machine's zone.
	TZ string `json:"tz,omitempty"`
	// Prompt is the user message each run sends.
	Prompt string `json:"prompt"`
	// By is ChatSchedByUser or ChatSchedByAI.
	By          string `json:"by,omitempty"`
	CreatedAtMs int64  `json:"created_at_ms,omitempty"`
	// LastRunMs is the last slot handled and LastOutcome what happened.
	LastRunMs   int64  `json:"last_run_ms,omitempty"`
	LastOutcome string `json:"last_outcome,omitempty"`
}

// ChatScheduleSpec reads a schedule text. "at HH:MM" (and a bare
// "HH:MM") is every day at that time; the rest is the workflow
// schedule's one-line form.
func ChatScheduleSpec(when, tz string) (schedule.Spec, error) {
	w := strings.TrimSpace(when)
	if w == "" {
		return schedule.Spec{}, errors.New(`say when, for example "at 09:00" or "weekdays 09:00"`)
	}
	f := strings.Fields(w)
	if strings.EqualFold(f[0], "at") && len(f) == 2 {
		w = "daily " + f[1]
	} else if len(f) == 1 && strings.Contains(f[0], ":") && !strings.HasPrefix(f[0], ":") {
		w = "daily " + f[0]
	}
	sp, err := schedule.ParseText(w)
	if err != nil {
		return schedule.Spec{}, err
	}
	sp.TZ = tz
	return sp, nil
}

// CompileChatSchedule checks the schedule and returns it compiled. A
// schedule that runs more often than ChatSchedMinGap is refused.
func CompileChatSchedule(when, tz string, now time.Time) (*schedule.Schedule, error) {
	sp, err := ChatScheduleSpec(when, tz)
	if err != nil {
		return nil, err
	}
	s, err := schedule.Compile(sp)
	if err != nil {
		return nil, err
	}
	t := s.Next(now)
	if t.IsZero() {
		return nil, errors.New("this schedule never runs")
	}
	// The gap check looks at a day of slots: DST may make one gap short
	// by an hour, so a little slack is allowed.
	for i := 0; i < 30; i++ {
		n := s.Next(t)
		if n.IsZero() {
			break
		}
		if n.Sub(t) < ChatSchedMinGap-time.Minute && !dstNear(t, n, s.Location()) {
			return nil, fmt.Errorf("a chat runs at most once an hour (each run is a paid turn); %q runs more often", strings.TrimSpace(when))
		}
		t = n
	}
	return s, nil
}

// dstNear: the zone changed between a and b (a short gap is DST, not
// the schedule).
func dstNear(a, b time.Time, loc *time.Location) bool {
	_, oa := a.In(loc).Zone()
	_, ob := b.In(loc).Zone()
	return oa != ob
}

// Check validates a whole chat schedule (the When text, the zone and
// the prompt).
func (c ChatSchedule) Check(now time.Time) (*schedule.Schedule, error) {
	if strings.TrimSpace(c.Prompt) == "" {
		return nil, errors.New("the message to send is empty")
	}
	if utf8.RuneCountInString(c.Prompt) > ChatSchedPromptMax {
		return nil, fmt.Errorf("the message is longer than %d characters", ChatSchedPromptMax)
	}
	return CompileChatSchedule(c.When, c.TZ, now)
}

// Label is the short form people see: "daily 09:00" (+ zone).
func (c ChatSchedule) Label() string {
	w := strings.Join(strings.Fields(c.When), " ")
	if f := strings.Fields(w); len(f) == 2 && strings.EqualFold(f[0], "at") {
		w = "daily " + f[1]
	} else if len(f) == 1 && strings.Contains(w, ":") && !strings.HasPrefix(w, ":") {
		w = "daily " + w
	}
	if c.TZ != "" {
		w += " " + c.TZ
	}
	return w
}

// ScheduledChats are the chats in the store that have a schedule.
func (s ChatStore) ScheduledChats() []Conversation {
	var out []Conversation
	for _, c := range s.List() {
		if c.Schedule != nil {
			out = append(out, c)
		}
	}
	return out
}
