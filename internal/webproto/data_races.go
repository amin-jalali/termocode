package webproto

// Races (MOB-61, mobile plan §3.9, AGX-4…6): the phone sees the agent
// races of the laptop's repository — each race's task, its 2–4 lanes
// (agent, state, files and lines changed, the verify result, the cost
// when known), the AI's short difference summary and the merged lane.
// It is read only: start, summarise, verify and stop are their own ops
// (MOB-62); merge and discard stay on the laptop. The topic never sends
// a lane's folder, a file's contents or the verify command line (only
// whether one is set). The topic sends the whole list.

func init() {
	registerTopic("races", RacesSnap{}, RacesSnap{})
}

// Races limits.
const (
	MaxRaces      = 20  // races listed, newest first (agentrace keeps 20)
	MaxRaceLanes  = 4   // lanes of one race
	MaxRaceFiles  = 50  // changed files listed per lane (Files counts all)
	MaxRacePrompt = 300 // bytes of a race's task line
	MaxRaceLines  = 3   // summary lines
	MaxRaceLine   = 200 // bytes of one summary line or note
)

// RacesSnap is the races topic. State: "none" the workspace is not a
// git repository, "loading" before the laptop read the records, "error"
// the records could not be read (the laptop says why), "ready".
type RacesSnap struct {
	State string     `json:"state" enum:"none|loading|error|ready"`
	Races []RaceItem `json:"races"`
}

// RaceItem is one race. State: "working" while a lane works or waits,
// "done" when every open lane is finished, "closed" when a lane was
// merged or every lane is closed. Started is Unix ms.
type RaceItem struct {
	ID          string     `json:"id"`
	Number      int        `json:"number"`
	Name        string     `json:"name"`
	Prompt      string     `json:"prompt"`
	Verify      bool       `json:"verify,omitempty"`
	Started     int64      `json:"started"`
	State       string     `json:"state" enum:"working|done|closed"`
	Lanes       []RaceLane `json:"lanes"`
	Summary     []string   `json:"summary,omitempty"`
	SummaryCost string     `json:"summary_cost,omitempty"`
	Winner      string     `json:"winner,omitempty"`
}

// RaceLane is one lane. Stats: Files, Add and Del were read (no files is
// "no changes"); Changed lists at most MaxRaceFiles of them. Verify is
// the verify command's state in the lane and Exit its code when it
// failed. Cost is "$0.42" when the agent says ("" unknown).
type RaceLane struct {
	ID      string     `json:"id"`
	Agent   string     `json:"agent"`
	Title   string     `json:"title"`
	State   string     `json:"state" enum:"starting|working|waiting|done|stopped|failed|merged|discarded"`
	Branch  string     `json:"branch,omitempty"`
	Stats   bool       `json:"stats,omitempty"`
	Files   int        `json:"files,omitempty"`
	Add     int        `json:"add,omitempty"`
	Del     int        `json:"del,omitempty"`
	Changed []RaceFile `json:"changed,omitempty"`
	Verify  string     `json:"verify,omitempty" enum:"running|pass|fail"`
	Exit    int        `json:"exit,omitempty"`
	Cost    string     `json:"cost,omitempty"`
	Note    string     `json:"note,omitempty"`
}

// RaceFile is one file a lane changed, by its path in the repository.
type RaceFile struct {
	Status string `json:"status" enum:"A|M|D"`
	Path   string `json:"path"`
	Add    int    `json:"add,omitempty"`
	Del    int    `json:"del,omitempty"`
	Binary bool   `json:"binary,omitempty"`
}
