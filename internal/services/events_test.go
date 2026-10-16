package services_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

func TestWatchTicksAndStops(t *testing.T) {
	c, f := newCompose(t)
	ch, stop, err := c.Watch(context.Background(), "termocode")
	if err != nil {
		t.Fatal(err)
	}
	if !servicestest.WaitFor(5*time.Second, func() bool { return f.PID("events.pid") > 0 }) {
		t.Fatal("events never started")
	}
	pid := f.PID("events.pid")
	calls := f.Calls()
	if got := calls[len(calls)-1]; got != "events --format {{json .}} --filter type=container --filter label=com.docker.compose.project=termocode" {
		t.Errorf("events argv = %q", got)
	}
	time.Sleep(100 * time.Millisecond) // tail is reading
	f.PushEvent(t, `{"Type":"container","Action":"start","Actor":{"Attributes":{"com.docker.compose.service":"postgres"}}}`)
	select {
	case ev := <-ch:
		if ev.Action != "start" || ev.Service != "postgres" {
			t.Errorf("event = %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no tick for an event line")
	}
	f.PushEvent(t, `{"Type":"container","Action":"health_status: healthy","Actor":{"Attributes":{"com.docker.compose.service":"postgres"}}}`)
	select {
	case ev := <-ch:
		if !strings.HasPrefix(ev.Action, "health_status") {
			t.Errorf("event = %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no second tick")
	}
	stop()
	stop() // twice is fine
	if !servicestest.WaitGone(pid, 3*time.Second) {
		t.Error("docker events still runs after stop")
	}
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel must close after stop")
		}
	case <-time.After(3 * time.Second):
		t.Error("channel not closed")
	}
}

func TestWatchContextCancel(t *testing.T) {
	c, f := newCompose(t)
	ctx, cancel := context.WithCancel(context.Background())
	ch, stop, err := c.Watch(ctx, "p")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	servicestest.WaitFor(5*time.Second, func() bool { return f.PID("events.pid") > 0 })
	pid := f.PID("events.pid")
	cancel()
	if !servicestest.WaitGone(pid, 3*time.Second) {
		t.Error("cancel did not kill docker events")
	}
	servicestest.WaitFor(3*time.Second, func() bool {
		select {
		case _, ok := <-ch:
			return !ok
		default:
			return false
		}
	})
	_ = services.Event{}
}
