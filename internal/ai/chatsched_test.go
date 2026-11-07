package ai

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// CMP-43: chat schedules.

func TestChatScheduleSpecAtIsDaily(t *testing.T) {
	for _, in := range []string{"at 09:00", "AT 09:00", "09:00"} {
		sp, err := ChatScheduleSpec(in, "UTC")
		if err != nil || sp.Preset != "daily" || sp.Time != "09:00" || sp.TZ != "UTC" {
			t.Fatalf("%q: %+v %v", in, sp, err)
		}
	}
	sp, err := ChatScheduleSpec("weekly mon 08:30", "")
	if err != nil || sp.Preset != "weekly" || sp.Day != "mon" {
		t.Fatalf("weekly: %+v %v", sp, err)
	}
	if _, err := ChatScheduleSpec("  ", ""); err == nil {
		t.Fatal("empty schedule accepted")
	}
}

func TestCompileChatScheduleAtMostHourly(t *testing.T) {
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	for _, ok := range []string{"at 09:00", "weekdays 09:00", "hourly :15", "0 */2 * * *"} {
		if _, err := CompileChatSchedule(ok, "UTC", now); err != nil {
			t.Fatalf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"*/10 * * * *", "0,30 * * * *", "* * * * *"} {
		_, err := CompileChatSchedule(bad, "UTC", now)
		if err == nil || !strings.Contains(err.Error(), "once an hour") {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	if _, err := CompileChatSchedule("at 9", "UTC", now); err == nil {
		t.Fatal("bad time accepted")
	}
	if _, err := CompileChatSchedule("at 09:00", "Not/AZone", now); err == nil {
		t.Fatal("bad zone accepted")
	}
}

func TestCompileChatScheduleNextRun(t *testing.T) {
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC) // a Saturday
	s, err := CompileChatSchedule("at 09:00", "UTC", now)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := s.Next(now), time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("next %v, want %v", got, want)
	}
	s, _ = CompileChatSchedule("weekdays 09:00", "UTC", now)
	if got, want := s.Next(now), time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("weekdays next %v, want %v", got, want)
	}
}

func TestChatScheduleCheckPrompt(t *testing.T) {
	now := time.Now()
	if _, err := (ChatSchedule{When: "at 09:00", Prompt: "  "}).Check(now); err == nil {
		t.Fatal("empty prompt accepted")
	}
	long := strings.Repeat("x", ChatSchedPromptMax+1)
	if _, err := (ChatSchedule{When: "at 09:00", Prompt: long}).Check(now); err == nil {
		t.Fatal("long prompt accepted")
	}
	if _, err := (ChatSchedule{When: "at 09:00", Prompt: "check CI"}).Check(now); err != nil {
		t.Fatal(err)
	}
}

func TestChatScheduleLabel(t *testing.T) {
	cases := map[ChatSchedule]string{
		{When: "at 09:00"}:                   "daily 09:00",
		{When: "09:00", TZ: "Europe/Berlin"}: "daily 09:00 Europe/Berlin",
		{When: "  weekdays   09:00 "}:        "weekdays 09:00",
		{When: "0 9 * * 1"}:                  "0 9 * * 1",
	}
	for in, want := range cases {
		if got := in.Label(); got != want {
			t.Fatalf("%+v: %q, want %q", in, got, want)
		}
	}
}

func TestChatStoreKeepsScheduledChatsOnPrune(t *testing.T) {
	s := ChatStore{Dir: t.TempDir()}
	old := NewConversation()
	old.Messages = []StoredMessage{{Role: StoredUser, Text: "check the queue"}}
	old.Schedule = &ChatSchedule{When: "at 09:00", Prompt: "check the queue", By: ChatSchedByUser}
	if err := s.Save(&old); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	for i := 0; i < maxChats+3; i++ {
		c := NewConversation()
		c.ID = fmt.Sprintf("%s-%d", c.ID, i)
		c.Messages = []StoredMessage{{Role: StoredUser, Text: "hi"}}
		if err := s.Save(&c); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Load(old.ID)
	if err != nil {
		t.Fatalf("scheduled chat pruned: %v", err)
	}
	if got.Schedule == nil || got.Schedule.Prompt != "check the queue" {
		t.Fatalf("schedule lost: %+v", got.Schedule)
	}
	if n := len(s.ScheduledChats()); n != 1 {
		t.Fatalf("scheduled chats %d", n)
	}
	entries, _ := os.ReadDir(s.Dir)
	files := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			files++
		}
	}
	if files != maxChats+1 {
		t.Fatalf("%d chat files, want %d (50 + the scheduled one)", files, maxChats+1)
	}
}
