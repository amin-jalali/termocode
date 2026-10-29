package crash

import (
	"fmt"
	"reflect"
	"runtime/debug"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// Notice tells the UI that a panic was recovered in Where; Report is the
// report file ("" when it could not be written).
type Notice struct {
	Where  string
	Report string
}

var notices = make(chan Notice, 16)

// Notices delivers the recovered panics to the UI (it shows a toast).
// Full channel: the notice is dropped, the report is still written.
func Notices() <-chan Notice { return notices }

// Recovered writes the report of a recovered panic and tells the UI. It
// returns the report path.
func Recovered(where string, r any, stack []byte) string {
	path, err := WriteReport(where, r, stack)
	if err != nil {
		path = ""
	}
	select {
	case notices <- Notice{Where: where, Report: path}:
	default:
	}
	return path
}

// Guard recovers a panic of the goroutine it is deferred in:
//
//	defer crash.Guard("name")
func Guard(where string) {
	if r := recover(); r != nil {
		Recovered(where, r, debug.Stack())
	}
}

// Go runs fn in a new goroutine; a panic in it is recovered and reported
// instead of ending the process.
func Go(where string, fn func()) {
	go func() {
		defer Guard(where)
		injected(where)
		fn()
	}()
}

// GoWith is Go with a cleanup that runs after a recovered panic (close
// the connection the goroutine served, end what it was driving).
func GoWith(where string, fn func(), onPanic func()) {
	go func() {
		defer guardThen(where, onPanic)
		injected(where)
		fn()
	}()
}

func guardThen(where string, after func()) {
	if r := recover(); r != nil {
		Recovered(where, r, debug.Stack())
		if after != nil {
			after()
		}
	}
}

var (
	hookMu sync.Mutex
	hooks  map[string]int
)

// PanicIn makes the next goroutine started by Go or GoWith(where, …)
// panic before its work (tests only). undo removes it.
func PanicIn(where string) (undo func()) {
	hookMu.Lock()
	if hooks == nil {
		hooks = map[string]int{}
	}
	hooks[where]++
	hookMu.Unlock()
	return func() {
		hookMu.Lock()
		delete(hooks, where)
		hookMu.Unlock()
	}
}

func injected(where string) {
	hookMu.Lock()
	n := hooks[where]
	if n > 0 {
		hooks[where] = n - 1
	}
	hookMu.Unlock()
	if n > 0 {
		panic(fmt.Sprintf("test panic in %s", where))
	}
}

var cmdType = reflect.TypeOf(tea.Cmd(nil))

// cmdList returns the commands of a batch or a sequence message (both are
// slices of tea.Cmd; the sequence type is not exported).
func cmdList(msg tea.Msg) ([]tea.Cmd, bool) {
	if msg == nil {
		return nil, false
	}
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice || v.Type().Elem() != cmdType {
		return nil, false
	}
	out := make([]tea.Cmd, v.Len())
	for i := range out {
		out[i], _ = v.Index(i).Interface().(tea.Cmd)
	}
	return out, true
}

// GuardCmd wraps cmd so a panic in it is reported and gives no message
// instead of ending the program. The commands of a batch or a sequence it
// returns are wrapped too.
func GuardCmd(where string, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				Recovered(where, r, debug.Stack())
				msg = nil
			}
		}()
		msg = cmd()
		cmds, ok := cmdList(msg)
		if !ok {
			return msg
		}
		v := reflect.MakeSlice(reflect.TypeOf(msg), len(cmds), len(cmds))
		for i, c := range cmds {
			if g := GuardCmd(where, c); g != nil {
				v.Index(i).Set(reflect.ValueOf(g))
			}
		}
		return v.Interface()
	}
}
