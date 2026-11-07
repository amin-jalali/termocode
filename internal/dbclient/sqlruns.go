package dbclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/jsonc"
)

// DBX-45: saved SQL runs. A run is a script file played on one or many
// connections by their command line clients (psql, mysql, sqlite3,
// redis-cli), after optional before-launch steps, with the output also
// added to a log file. They live in .termocode/sql-runs.json:
//
//	{"runs": [{"name": "nightly report", "script": "sql/report.sql",
//	  "connections": ["local", "staging"], "before": ["make seed"],
//	  "log": "logs/report.log"}]}
//
// Nothing here starts a process: the app runs the shell line that
// SQLRunShell builds in a terminal tab.

// SQLRun is one saved run.
type SQLRun struct {
	Name        string   `json:"name"`
	Script      string   `json:"script"`
	Connections []string `json:"connections"` // connection ids (or names)
	Before      []string `json:"before,omitempty"`
	Log         string   `json:"log,omitempty"`
}

// SQLRunTarget is one connection of a run and its client command.
type SQLRunTarget struct {
	Name string
	CLI  CLICmd
}

// SQLRunsPath is the file of the saved runs of a workspace.
func SQLRunsPath(root string) string {
	return filepath.Join(root, ".termocode", "sql-runs.json")
}

// LoadSQLRuns reads the saved runs; a missing file is no runs.
func LoadSQLRuns(root string) ([]SQLRun, error) {
	b, err := os.ReadFile(SQLRunsPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseSQLRuns(b)
}

// ParseSQLRuns parses and checks sql-runs.json (comments allowed).
func ParseSQLRuns(data []byte) ([]SQLRun, error) {
	var f struct {
		Runs []SQLRun `json:"runs"`
	}
	if err := json.Unmarshal(jsonc.Strip(data), &f); err != nil {
		return nil, fmt.Errorf("sql-runs.json: %w", err)
	}
	seen := map[string]bool{}
	for i, r := range f.Runs {
		where := fmt.Sprintf("sql-runs.json: run %d", i+1)
		name := strings.TrimSpace(r.Name)
		switch {
		case name == "":
			return nil, errors.New(where + ": name is required")
		case seen[name]:
			return nil, fmt.Errorf("%s: the name %q is used twice", where, name)
		case strings.TrimSpace(r.Script) == "":
			return nil, fmt.Errorf("%s (%s): script is required", where, name)
		case len(r.Connections) == 0:
			return nil, fmt.Errorf("%s (%s): at least one connection is required", where, name)
		}
		for _, p := range []string{r.Script, r.Log} {
			if strings.ContainsAny(p, "\x00\n\r") {
				return nil, fmt.Errorf("%s (%s): a path has a control character", where, name)
			}
		}
		for _, s := range r.Before {
			if strings.TrimSpace(s) == "" || strings.ContainsAny(s, "\x00\n\r") {
				return nil, fmt.Errorf("%s (%s): a before step is empty or has a line break", where, name)
			}
		}
		seen[name] = true
		f.Runs[i].Name = name
	}
	return f.Runs, nil
}

// SQLRunPath is p made absolute against root.
func SQLRunPath(root, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}

var envKeyPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// SQLRunShell builds the POSIX shell line of a run and the environment
// it needs. The before steps run first, then the script on each target
// in order; the first failure stops the run (set -e). With a log file
// all output is also appended to it. Passwords and SSL settings are in
// env under TC_SQLRUN_<n>_<KEY> and only referenced on the line
// ("$TC_SQLRUN_1_PGPASSWORD"), so they never show in the process list.
func SQLRunShell(r SQLRun, targets []SQLRunTarget, root string) (string, map[string]string, error) {
	if len(targets) == 0 {
		return "", nil, errors.New("no connection to run on")
	}
	script := SQLRunPath(root, r.Script)
	env := map[string]string{}
	parts := []string{"set -e"}
	for _, s := range r.Before {
		parts = append(parts, "printf '%s\\n' "+shq("$ "+s), s)
	}
	for i, t := range targets {
		if t.CLI.Tool == "" {
			return "", nil, fmt.Errorf("%s: no command line client", t.Name)
		}
		seg := []string{}
		for _, kv := range t.CLI.Env {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || !envKeyPattern.MatchString(k) {
				return "", nil, fmt.Errorf("%s: bad environment entry", t.Name)
			}
			name := "TC_SQLRUN_" + strconv.Itoa(i+1) + "_" + k
			env[name] = v
			seg = append(seg, k+`="$`+name+`"`)
		}
		seg = append(seg, shq(t.CLI.Tool))
		for _, a := range t.CLI.Args {
			seg = append(seg, shq(a))
		}
		seg = append(seg, "<", shq(script))
		parts = append(parts, "printf '\\n== %s ==\\n' "+shq(t.Name), strings.Join(seg, " "))
	}
	parts = append(parts, "printf '\\n== done ==\\n'")
	line := "{ " + strings.Join(parts, "; ") + "; } 2>&1"
	if r.Log != "" {
		log := SQLRunPath(root, r.Log)
		line = "mkdir -p " + shq(filepath.Dir(log)) + " && " + line + " | tee -a " + shq(log)
	}
	return line, env, nil
}

// SQLRunSummary is the plain text of a run for the confirm card.
func SQLRunSummary(r SQLRun, names []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Script: %s\n", r.Script)
	fmt.Fprintf(&b, "On %d connection(s), in order: %s\n", len(names), strings.Join(names, ", "))
	if len(r.Before) > 0 {
		b.WriteString("First runs these commands:\n")
		for _, s := range r.Before {
			b.WriteString("  $ " + s + "\n")
		}
	}
	if r.Log != "" {
		fmt.Fprintf(&b, "Output is also added to %s\n", r.Log)
	}
	b.WriteString("The first error stops the run.")
	return b.String()
}

// shq quotes s for a POSIX shell.
func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
