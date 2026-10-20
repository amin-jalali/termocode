package app

import (
	"context"
	"net"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
)

// DB-16: the Databases view finds servers itself. On open and on R it
// reads the container list once (the same engine code as the Containers
// view) and tries one short TCP connect to a few well-known ports on this
// computer. No polling, no events stream, nothing while the view is hidden.

// dbProbeTarget is a local port that suggests a database server.
type dbProbeTarget struct {
	port   int
	driver string
}

// dbProbeTargets is the list of ports tried on 127.0.0.1 (tests change it).
var dbProbeTargets = []dbProbeTarget{{5432, "postgres"}, {3306, "mysql"}, {6379, "redis"}}

// dbProbeHost is the only host that is ever probed.
var dbProbeHost = "127.0.0.1"

var dbProbeTimeout = 300 * time.Millisecond

// dbListContainers reads the container list; tests replace it with a fake.
// (nil, nil) means no engine was found.
var dbListContainers = func(ctx context.Context, dir string) ([]containers.Container, error) {
	e := containers.Detect(ctx, containers.DetectOptions{Exec: servicesExec, Dir: dir, Android: runtime.GOOS == "android"})
	if !e.Reachable {
		return nil, nil
	}
	return e.ListContainers(ctx)
}

type dbDiscoverMsg struct {
	generation uint64
	containers []containers.Container
	haveList   bool
	ports      []int
}

// databaseDiscover starts one discovery in the background. It returns nil
// when one is already running, or when it is switched off.
func (m *Model) databaseDiscover() tea.Cmd {
	s := m.databases
	if s == nil || (s.discovering && s.discoverGen == s.generation) || os.Getenv("TERMOCODE_NO_DB_DISCOVERY") != "" {
		return nil
	}
	s.discovering, s.discoverGen = true, s.generation
	generation, dir := s.generation, s.root
	targets := append([]dbProbeTarget(nil), dbProbeTargets...)
	host, timeout, list := dbProbeHost, dbProbeTimeout, dbListContainers
	return func() tea.Msg {
		msg := dbDiscoverMsg{generation: generation}
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, t := range targets {
			wg.Add(1)
			go func(port int) {
				defer wg.Done()
				c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
				if err != nil {
					return
				}
				_ = c.Close()
				mu.Lock()
				msg.ports = append(msg.ports, port)
				mu.Unlock()
			}(t.port)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if rows, err := list(ctx, dir); err == nil {
				msg.containers, msg.haveList = rows, true
			}
		}()
		wg.Wait()
		sort.Ints(msg.ports)
		return msg
	}
}

func (m *Model) onDBDiscover(msg dbDiscoverMsg) tea.Cmd {
	s := m.databases
	if s == nil || msg.generation != s.generation {
		return nil
	}
	s.discovering = false
	s.discoverDone = true
	if msg.haveList {
		s.discovered = msg.containers
	}
	s.discoveredPorts = msg.ports
	s.dropDiscoverySuggestions()
	s.addRuntimeSuggestions(m.services)
	s.fixCursor()
	s.touch()
	return nil
}
