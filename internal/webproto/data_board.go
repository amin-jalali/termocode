package webproto

import (
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// The live board (MOB-18, mobile plan §2.5 and §3.8; MOBUX-9): one view
// of what goes on in the home project, in four columns — needs you,
// working, review, done today. It is built on the laptop from the
// topics the page already has (cards, runs, worktrees, workflows), so it
// carries nothing they do not carry and no new write path: every item
// names its source (Kind) and its id there (Ref), and a tap opens that
// source's own view. board.get answers the board now; the page asks
// again when one of those topics changes (at most twice a second), so
// the board is live without a topic of its own. MOB-54 builds the same
// board for every project.

// OpBoardGet is the board now.
const OpBoardGet = "board.get"

// Board limits.
const (
	MaxBoardItems = 200 // items on the board; the rest are counted in More
	MaxBoardText  = 120 // bytes of a title, sub or why line
)

// Board columns, in the order the page draws them.
const (
	BoardNeedsYou = "needs_you"
	BoardWorking  = "working"
	BoardReview   = "review"
	BoardDone     = "done"
)

// BoardColumns lists the columns in drawing order.
var BoardColumns = []string{BoardNeedsYou, BoardWorking, BoardReview, BoardDone}

// Board kinds: the topic an item comes from.
const (
	BoardKindCard     = "card"
	BoardKindRun      = "run"
	BoardKindLane     = "lane"
	BoardKindWorkflow = "workflow"
)

// Board states: the page draws each with a color and a shape
// (wait ▲, run ◐, ok ✓, fail ✗, idle ○).
const (
	BoardWait = "wait"
	BoardRun  = "run"
	BoardOK   = "ok"
	BoardFail = "fail"
	BoardIdle = "idle"
)

// Board is the result of board.get.
type Board struct {
	Items  []BoardItem `json:"items"`
	Counts BoardCounts `json:"counts"`
	// TodayUSD is the known cost of the AI runs started today.
	TodayUSD float64 `json:"today_usd,omitempty"`
	// More counts the items past MaxBoardItems (Counts has them all).
	More int `json:"more,omitempty"`
	// Built is when the laptop built it (Unix ms).
	Built int64 `json:"built"`
}

// BoardCounts counts the items of each column.
type BoardCounts struct {
	NeedsYou int `json:"needs_you"`
	Working  int `json:"working"`
	Review   int `json:"review"`
	Done     int `json:"done"`
}

// BoardItem is one tile of the board. ID is unique on the board
// ("run:<key>"); Ref is the id in its own topic (a card id, a run key, a
// worktree id, a workflow run id).
type BoardItem struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind" enum:"card|run|lane|workflow"`
	Column  string  `json:"column" enum:"needs_you|working|review|done"`
	State   string  `json:"state" enum:"wait|run|ok|fail|idle"`
	Title   string  `json:"title"`
	Sub     string  `json:"sub,omitempty"` // agent, profile, branch or workflow
	Why     string  `json:"why,omitempty"` // why it waits
	Step    int     `json:"step,omitempty"`
	Steps   int     `json:"steps,omitempty"`
	CostUSD float64 `json:"cost_usd,omitempty"`
	Ahead   int     `json:"ahead,omitempty"` // lane: commits ahead of its base
	Dirty   int     `json:"dirty,omitempty"` // lane: changed files
	Since   int64   `json:"since,omitempty"` // Unix ms it started or last moved
	Ref     string  `json:"ref"`
}

// BoardInput is what the board is built from: the topics' data now.
type BoardInput struct {
	Cards     CardsSnap
	Runs      RunsSnap
	Worktrees WorktreesSnap
	Workflows WorkflowsSnap
	Now       time.Time // its zone says when today began
}

var opBoardRows = []Policy{
	{Op: OpBoardGet, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opBoardRows...); return true }()

func init() {
	registerResult(OpBoardGet, Board{})
}

// boardText cuts a line of the board to MaxBoardText bytes, on a
// character edge, on one line.
func boardText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= MaxBoardText {
		return s
	}
	s = s[:MaxBoardText]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// BuildBoard sorts the topics' items into the board's columns. Needs you
// lists the oldest first (it waited longest); the other columns the
// newest first. Done holds what ended today only. Pure: no clock, no
// files.
func BuildBoard(in BoardInput) Board {
	y, mo, d := in.Now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, in.Now.Location()).UnixMilli()
	var all []BoardItem
	add := func(it BoardItem) {
		it.Title, it.Sub, it.Why = boardText(it.Title), boardText(it.Sub), boardText(it.Why)
		all = append(all, it)
	}
	for _, c := range in.Cards.Cards {
		add(BoardItem{ID: "card:" + c.ID, Kind: BoardKindCard, Column: BoardNeedsYou, State: BoardWait, Title: c.Title, Sub: c.Kind, Since: c.Since, Ref: c.ID})
	}
	b := Board{Items: []BoardItem{}, Built: in.Now.UnixMilli()}
	for _, r := range in.Runs.Runs {
		it := BoardItem{ID: "run:" + r.Key, Kind: BoardKindRun, Title: r.Title, Sub: r.Profile, Why: r.Why,
			Step: r.Step, Steps: r.Steps, CostUSD: r.CostUSD, Since: r.Started, Ref: r.Key}
		if it.Sub == "" {
			it.Sub = r.Chat
		}
		if r.Started >= today {
			b.TodayUSD += r.CostUSD
		}
		switch {
		case r.NeedsYou || r.State == "paused" || r.State == "resumable":
			it.Column, it.State = BoardNeedsYou, BoardWait
		case r.State == "running" || r.State == "stalled" || r.State == "pausing":
			it.Column, it.State = BoardWorking, BoardRun
		case r.Started < today:
			continue
		case r.State == "done":
			it.Column, it.State = BoardDone, BoardOK
		case r.State == "failed":
			it.Column, it.State = BoardDone, BoardFail
		default: // cancelled
			it.Column, it.State = BoardDone, BoardIdle
		}
		add(it)
	}
	for _, w := range in.Worktrees.Worktrees {
		if w.Kind == "main" || w.Kind == "external" || w.Gone || (w.Group != "active" && w.Group != "pinned") {
			continue
		}
		it := BoardItem{ID: "lane:" + w.ID, Kind: BoardKindLane, Title: w.Title, Sub: w.Agent, Why: w.Note,
			Ahead: w.Ahead, Dirty: w.Dirty, Since: w.LastSeen, Ref: w.ID}
		if it.Sub == "" {
			it.Sub = w.Branch
		}
		switch w.State {
		case "waiting":
			it.Column, it.State = BoardNeedsYou, BoardWait
		case "working":
			it.Column, it.State = BoardWorking, BoardRun
		case "done":
			it.Column, it.State = BoardReview, BoardOK
		default:
			continue
		}
		add(it)
	}
	for _, r := range in.Workflows.Runs {
		it := BoardItem{ID: "workflow:" + r.ID, Kind: BoardKindWorkflow, Title: r.Workflow, Since: r.Started, Ref: r.ID}
		if r.Dry {
			it.Sub = "dry run"
		}
		ended := r.Ended
		if ended == 0 {
			ended = r.Started
		}
		switch {
		case r.Waiting != "":
			it.Column, it.State, it.Why = BoardNeedsYou, BoardWait, "waits: "+r.Waiting
		case r.State == "paused" || r.State == "interrupted":
			it.Column, it.State = BoardNeedsYou, BoardWait
		case r.State == "running":
			it.Column, it.State = BoardWorking, BoardRun
		case ended < today:
			continue
		case r.State == "done":
			it.Column, it.State = BoardDone, BoardOK
			if r.Failed > 0 {
				it.State = BoardFail
			}
		case r.State == "failed":
			it.Column, it.State = BoardDone, BoardFail
		default: // cancelled
			it.Column, it.State = BoardDone, BoardIdle
		}
		add(it)
	}
	order := map[string]int{BoardNeedsYou: 0, BoardWorking: 1, BoardReview: 2, BoardDone: 3}
	sort.SliceStable(all, func(i, j int) bool {
		a, c := all[i], all[j]
		if a.Column != c.Column {
			return order[a.Column] < order[c.Column]
		}
		if a.Since != c.Since {
			if a.Column == BoardNeedsYou {
				return a.Since < c.Since
			}
			return a.Since > c.Since
		}
		return a.ID < c.ID
	})
	for _, it := range all {
		switch it.Column {
		case BoardNeedsYou:
			b.Counts.NeedsYou++
		case BoardWorking:
			b.Counts.Working++
		case BoardReview:
			b.Counts.Review++
		case BoardDone:
			b.Counts.Done++
		}
	}
	if len(all) > MaxBoardItems {
		b.More = len(all) - MaxBoardItems
		all = all[:MaxBoardItems]
	}
	b.Items = append(b.Items, all...)
	b.TodayUSD = float64(int64(b.TodayUSD*100+0.5)) / 100
	return b
}
