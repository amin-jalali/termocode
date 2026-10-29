package services

import (
	"bufio"
	"context"
	"encoding/json"

	"github.com/amin-jalali/termocode/internal/crash"
)

// Event is one `docker events` line, reduced to what the app uses.
type Event struct {
	Type    string
	Action  string // start, die, health_status: healthy, …
	Service string // com.docker.compose.service
}

// Watch follows `docker events` for one compose project. Each line is
// one Event on the channel (dropped when the reader is behind: the app
// only needs "something changed"). The channel closes when the stream
// ends. stop kills docker and waits; call it when the view hides. While
// watching, one goroutine blocks on the pipe (no CPU).
func (c *Compose) Watch(ctx context.Context, project string) (<-chan Event, func(), error) {
	x := c.X
	if x == nil {
		x = SpawnExec{}
	}
	argv := []string{c.docker(), "events", "--format", "{{json .}}", "--filter", "type=container"}
	if project != "" {
		argv = append(argv, "--filter", "label=com.docker.compose.project="+project)
	}
	out, stop, err := x.Stream(ctx, c.Root, argv...)
	if err != nil {
		return nil, nil, err
	}
	ch := make(chan Event, 16)
	crash.Go("the services events", func() {
		defer close(ch)
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var raw struct {
				Type   string
				Action string
				Status string
				Actor  struct{ Attributes map[string]string }
			}
			ev := Event{}
			if json.Unmarshal(sc.Bytes(), &raw) == nil {
				ev = Event{Type: raw.Type, Action: raw.Action, Service: raw.Actor.Attributes["com.docker.compose.service"]}
				if ev.Action == "" {
					ev.Action = raw.Status
				}
			}
			select {
			case ch <- ev:
			default:
			}
		}
	})
	return ch, stop, nil
}
