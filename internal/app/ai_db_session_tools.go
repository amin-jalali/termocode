package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

// db_session: the AI controls the user's own session of the Databases
// view as the GUI does (DB parity): read its state, commit or roll back
// its open transaction, switch its commit mode, set a savepoint or roll
// back to one, and stop the console's running query.
//
// It asks exactly where the GUI asks: commit, roll back and the commit
// mode are the web's db.tx cards, roll back to a savepoint is the TUI's
// card; each asks every time, with no "Always allow". status, a new
// savepoint (the TUI only asks for its name) and cancel (Esc, db.cancel)
// run at once. The AI's own SQL still never runs on this session:
// db_query opens a session of its own.

// aiDBSessionOps are the ops; true: the op asks every time.
var aiDBSessionOps = map[string]bool{
	"status": false, "cancel": false, "savepoint": false,
	"commit": true, "rollback": true, "tx_mode": true, "rollback_savepoint": true,
}

func aiDBSessionCheck(root string, a map[string]any) (dbclient.Conn, error) {
	op := ai.ArgString(a, "op")
	if _, ok := aiDBSessionOps[op]; !ok {
		return dbclient.Conn{}, errors.New("give op: status, commit, rollback, tx_mode, savepoint, rollback_savepoint or cancel")
	}
	c, err := aiDBAnyConn(root, ai.ArgString(a, "conn"))
	if err != nil {
		return c, err
	}
	switch op {
	case "tx_mode":
		if m := ai.ArgString(a, "mode"); m != "manual" && m != "auto" {
			return c, errors.New("give mode: manual (changes wait for commit) or auto (each statement commits)")
		}
		fallthrough
	case "commit", "rollback", "savepoint", "rollback_savepoint":
		if c.Driver == "redis" {
			return c, errors.New("Redis has no transactions here")
		}
	}
	if op == "savepoint" || op == "rollback_savepoint" {
		if !dbSavepointName.MatchString(ai.ArgString(a, "name")) {
			return c, errors.New("give name: a savepoint name (letters, digits and _, starting with a letter)")
		}
	}
	return c, nil
}

func aiDBSessionTool(root string, call aiIDECaller) ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	ops := make([]string, 0, len(aiDBSessionOps))
	for op := range aiDBSessionOps {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	ui := func(ctx context.Context, fn func(m *Model) (string, tea.Cmd, error)) (string, error) {
		if call == nil {
			return "", errors.New("the Databases view is not available")
		}
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			m.ensureDatabasesLoaded()
			return fn(m)
		})
	}
	return ai.Tool{
		Name: "db_session",
		Description: "Control the user's own session of a connection in the Databases view (not yours: db_query has its own). " +
			"op status (commit mode, open transaction, uncommitted changes, savepoints, running query), savepoint (name) and cancel (stop the console's running query) run at once; " +
			"commit, rollback, tx_mode (mode manual|auto) and rollback_savepoint (name) ask the user every time.",
		Params: map[string]any{
			"op":   map[string]any{"type": "string", "enum": ops, "description": "what to do"},
			"conn": str("connection id (db_connections)"),
			"mode": map[string]any{"type": "string", "enum": []string{"manual", "auto"}, "description": "tx_mode: manual commit or auto-commit"},
			"name": str("savepoint / rollback_savepoint: the savepoint name"),
		},
		Required: []string{"op", "conn"},
		Describe: func(a map[string]any) string {
			c, err := aiDBSessionCheck(root, a)
			if err != nil {
				return "db_session (invalid: " + dbOneLine(err.Error()) + ")"
			}
			name := sanitizeDBText(dbConnName(c))
			switch ai.ArgString(a, "op") {
			case "commit":
				return "Commit the open transaction on " + name + "? Its uncommitted changes are saved."
			case "rollback":
				return "Roll back the open transaction on " + name + "? Its uncommitted changes are dropped."
			case "tx_mode":
				if ai.ArgString(a, "mode") == "manual" {
					return "Switch " + name + " to manual commit? Changes in the console wait for Commit or Roll back."
				}
				return "Switch " + name + " to auto-commit? Each statement commits by itself. With uncommitted changes, the Databases view asks first what to do with them."
			case "rollback_savepoint":
				return "Roll back to savepoint " + ai.ArgString(a, "name") + " on " + name + "? Every change after it is dropped. The transaction stays open."
			case "savepoint":
				return "Set savepoint " + ai.ArgString(a, "name") + " on " + name
			case "cancel":
				return "Stop the running query on " + name
			}
			return "Read the session state of " + name
		},
		Validate: func(a map[string]any) error { _, err := aiDBSessionCheck(root, a); return err },
		Gate: func(a map[string]any) (bool, string) {
			ask, ok := aiDBSessionOps[ai.ArgString(a, "op")]
			return ask || !ok, ""
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			c, err := aiDBSessionCheck(root, a)
			if err != nil {
				return "", err
			}
			id, op, name := c.ID, ai.ArgString(a, "op"), ai.ArgString(a, "name")
			switch op {
			case "status":
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					s := m.databases
					var b strings.Builder
					conn := "not connected"
					if s.sessions[id] != nil {
						conn = "connected"
					}
					mode := "auto-commit"
					if s.manualMode(id) {
						mode = "manual commit"
					}
					fmt.Fprintf(&b, "%s: %s, %s\n", id, conn, mode)
					keys := s.txKeys(id)
					if len(keys) == 0 {
						b.WriteString("no open transaction\n")
					}
					for _, k := range keys {
						t := s.tx(k)
						fmt.Fprintf(&b, "open transaction on %s: %s", sanitizeDBText(k), dbPlural(t.writes, "uncommitted change"))
						if len(t.savepoints) > 0 {
							b.WriteString("; savepoints: " + strings.Join(t.savepoints, ", "))
						}
						b.WriteString("\n")
					}
					if cs := s.console; cs != nil && cs.running != nil && cs.running.conn == id {
						b.WriteString("a query runs in the console\n")
					}
					return b.String(), nil, nil
				})
			case "cancel":
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					cs := m.databases.console
					running := cs != nil && (cs.running != nil && cs.running.conn == id || cs.job != nil && cs.job.conn == id)
					if !running {
						return "Nothing runs on " + id + " in the console.", nil, nil
					}
					return "stopped the running query on " + id, m.databaseCancelRun(), nil
				})
			case "tx_mode":
				manual := ai.ArgString(a, "mode") == "manual"
				return ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					s := m.databases
					if s.manualMode(id) == manual {
						return "", nil, errors.New("it is in that mode already")
					}
					writes := s.txWrites(id)
					cmd := m.databaseSetTxMode(id, manual)
					switch {
					case manual:
						return id + " is in manual commit now: changes in the console wait for commit or rollback", cmd, nil
					case writes > 0:
						return "the Databases view asks the user what to do with " + dbPlural(writes, "uncommitted change") + " first", cmd, nil
					}
					return id + " is in auto-commit now", cmd, nil
				})
			case "commit", "rollback":
				commit := op == "commit"
				var end tea.Cmd
				if _, err := ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					s := m.databases
					if len(s.txKeys(id)) == 0 {
						return "", nil, fmt.Errorf("no open transaction on %s", id)
					}
					if m.dbSessionBusy() {
						return "", nil, errors.New("an import runs on it; stop it first (op cancel)")
					}
					end = m.databaseEndTx(id, commit, nil)
					return "", nil, nil
				}); err != nil {
					return "", err
				}
				msg, ok := end().(dbTxDoneMsg) // the commit runs here, off the UI goroutine
				if !ok {
					return "", errors.New("the transaction could not be ended")
				}
				_, _ = ui(ctx, func(m *Model) (string, tea.Cmd, error) { return "", m.onDBTxDone(msg), nil })
				for _, e := range msg.errs {
					if e != nil {
						return "", dbAIError(e, "")
					}
				}
				if commit {
					return "Committed the open transaction on " + id, nil
				}
				return "Rolled back the open transaction on " + id, nil
			case "savepoint", "rollback_savepoint":
				back := op == "rollback_savepoint"
				var sess dbclient.Session
				var key string
				if _, err := ui(ctx, func(m *Model) (string, tea.Cmd, error) {
					s := m.databases
					keys := s.txKeys(id)
					switch {
					case len(keys) == 0:
						return "", nil, fmt.Errorf("no open transaction on %s: savepoints need manual commit mode and an open transaction", id)
					case len(keys) > 1:
						return "", nil, fmt.Errorf("%s has several open transactions; the user picks one in the Databases view", id)
					case m.dbSessionBusy() || (s.console != nil && s.console.running != nil):
						return "", nil, errors.New("a query is running; wait or stop it (op cancel)")
					}
					key, sess = keys[0], s.sessions[keys[0]]
					if back && !contains(s.tx(key).savepoints, name) {
						return "", nil, fmt.Errorf("no savepoint %s in this transaction (op status lists them)", name)
					}
					if sess == nil {
						return "", nil, fmt.Errorf("%s is not connected", id)
					}
					return "", nil, nil
				}); err != nil {
					return "", err
				}
				sql := "SAVEPOINT " + name
				if back {
					sql = "ROLLBACK TO SAVEPOINT " + name
				}
				rctx, cancel := context.WithTimeout(ctx, loadDBConsoleSettings().timeout())
				res := sess.Run(rctx, sql, dbclient.RunOptions{})
				cancel()
				msg := dbSavepointMsg{key: key, name: name, rollback: back, err: res.Err}
				_, _ = ui(ctx, func(m *Model) (string, tea.Cmd, error) { return "", m.onDBSavepoint(msg), nil })
				if res.Err != nil {
					return "", dbAIError(res.Err, "")
				}
				if back {
					return "Rolled back to savepoint " + name + " on " + id + "; the transaction is still open", nil
				}
				return "Savepoint " + name + " set on " + id, nil
			}
			return "", errors.New("unknown op")
		},
	}
}
