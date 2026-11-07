package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Admin policy (plan §6.9): workflows-policy.json, a machine-wide file
// an admin owns and the user's own file next to mcp-policy.json. Both
// apply; a deny rule in either blocks.
//
//	{"deny": [{"step": "agent"}, {"write": "http"}, {"schedule": true},
//	          {"workflow": "nightly-*"}]}
//
// A rule names one thing: a step type, a write target, schedules, or a
// workflow name glob. A file that is missing allows everything; a file
// that cannot be read or has a bad rule blocks every workflow (fail
// closed).

// SystemPolicyPath is the machine-wide policy file (a var for tests).
var SystemPolicyPath = defaultSystemPolicyPath()

func defaultSystemPolicyPath() string {
	switch runtime.GOOS {
	case "windows":
		if d := os.Getenv("ProgramData"); d != "" {
			return filepath.Join(d, "termocode", "workflows-policy.json")
		}
		return ""
	case "darwin":
		return "/Library/Application Support/termocode/workflows-policy.json"
	}
	return "/etc/termocode/workflows-policy.json"
}

// UserPolicyPath is the user's own policy file (in configDir/termocode).
func UserPolicyPath(configDir string) string {
	if configDir == "" {
		return ""
	}
	return filepath.Join(configDir, "termocode", "workflows-policy.json")
}

// PolicyRule is one deny rule; exactly one field is set.
type PolicyRule struct {
	Step     string `json:"step,omitempty"`     // a step type
	Write    string `json:"write,omitempty"`    // a write target
	Schedule bool   `json:"schedule,omitempty"` // no workflow runs on a schedule
	Workflow string `json:"workflow,omitempty"` // a name glob
}

type policyFile struct {
	path   string
	deny   []PolicyRule
	broken string // why the file blocks everything ("" = it does not)
}

// Policy is the loaded policy files.
type Policy struct{ files []policyFile }

// LoadPolicy reads the policy files; "" paths are skipped.
func LoadPolicy(paths ...string) Policy {
	var p Policy
	for _, fp := range paths {
		if fp == "" {
			continue
		}
		b, err := os.ReadFile(fp)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		pf := policyFile{path: fp}
		var raw struct {
			Deny []PolicyRule `json:"deny"`
		}
		switch {
		case err != nil:
			pf.broken = "it cannot be read"
		case json.Unmarshal(b, &raw) != nil:
			pf.broken = "it is not valid JSON"
		default:
			for _, r := range raw.Deny {
				if why := r.check(); why != "" {
					pf.broken = why
					break
				}
			}
			pf.deny = raw.Deny
		}
		p.files = append(p.files, pf)
	}
	return p
}

func (r PolicyRule) check() string {
	n := 0
	for _, set := range []bool{r.Step != "", r.Write != "", r.Schedule, r.Workflow != ""} {
		if set {
			n++
		}
	}
	switch {
	case n != 1:
		return "a deny rule must name exactly one of step, write, schedule, workflow"
	case r.Step != "" && !slices.Contains(StepTypes, r.Step):
		return "unknown step type " + r.Step
	case r.Write != "" && !slices.Contains(WriteTargets, r.Write):
		return "unknown write target " + r.Write
	case r.Workflow != "":
		if _, err := path.Match(r.Workflow, ""); err != nil {
			return "bad workflow glob " + r.Workflow
		}
	}
	return ""
}

// Blocked says why f may not run at all ("" = it may).
func (p Policy) Blocked(f *File) string {
	var why []string
	for _, pf := range p.files {
		if pf.broken != "" {
			why = append(why, "the policy file "+pf.path+" blocks every workflow: "+pf.broken)
			continue
		}
		for _, r := range pf.deny {
			if w := r.blocks(f); w != "" {
				why = append(why, w+" ("+pf.path+")")
			}
		}
	}
	return strings.Join(why, "; ")
}

// DeniesUnattended reports whether a policy file denies schedules. An
// event trigger with "ask": false runs unattended too, so under such a
// policy it asks on a card like "ask": true (CMP-38).
func (p Policy) DeniesUnattended() bool {
	for _, pf := range p.files {
		for _, r := range pf.deny {
			if r.Schedule {
				return true
			}
		}
	}
	return false
}

// BlockedSchedule says why f may not run on its schedule ("" = it may).
func (p Policy) BlockedSchedule(f *File) string {
	var why []string
	if w := p.Blocked(f); w != "" {
		why = append(why, w)
	}
	if f.Schedule != nil {
		for _, pf := range p.files {
			for _, r := range pf.deny {
				if r.Schedule {
					why = append(why, "schedules are denied by policy ("+pf.path+")")
				}
			}
		}
	}
	return strings.Join(why, "; ")
}

func (r PolicyRule) blocks(f *File) string {
	if r.Workflow != "" {
		if ok, _ := path.Match(r.Workflow, f.Name); ok {
			return "the workflow " + f.Name + " is denied by policy"
		}
		return ""
	}
	if r.Step == "" && r.Write == "" {
		return ""
	}
	hit := ""
	Walk(f.Steps, func(s *Step) bool {
		switch {
		case r.Step != "" && s.Type == r.Step:
			hit = r.Step + " steps are denied by policy (step " + s.ID + ")"
		case r.Write != "" && s.Write != nil && s.Write.To == r.Write:
			hit = "writes to " + r.Write + " are denied by policy (step " + s.ID + ")"
		}
		return hit == ""
	})
	return hit
}
