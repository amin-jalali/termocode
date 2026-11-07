package assets

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The phone's live board (MOBUX-9, mobile plan §2.5 and §3.8): four
// columns — needs you, working, review, done — that the user swipes
// between; a tile is project · agent/model · step 3/5 · cost · CI; a tap
// opens its logs, diff or chat. Checked in the source until the browser
// tests run the phone views.

func readBoardSrc(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The columns come in the board's order, what needs the user first, and
// match the column names of board.get (webproto BoardColumns).
func TestPhoneBoardColumnOrder(t *testing.T) {
	src := readBoardSrc(t, "src/phone/board.ts")
	m := regexp.MustCompile(`export const BOARD_COLUMNS[^=]*=\s*\[([^\]]*)\]`).FindStringSubmatch(src)
	if m == nil {
		t.Fatal("board.ts has no BOARD_COLUMNS")
	}
	var got []string
	for _, q := range regexp.MustCompile(`"([a-z_]+)"`).FindAllStringSubmatch(m[1], -1) {
		got = append(got, q[1])
	}
	want := []string{"needs_you", "working", "review", "done"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("BOARD_COLUMNS = %v, want %v", got, want)
	}
	if !strings.Contains(src, `"board.get"`) {
		t.Error("board.ts does not name the board.get op")
	}
}

// Every board state maps to a phone status, so each tile has a colour
// and a shape; a failed run is never drawn as done.
func TestPhoneBoardStatusOfEveryState(t *testing.T) {
	src := readBoardSrc(t, "src/phone/board.ts")
	body := regexp.MustCompile(`(?s)export function statusOf\(.*?\n\}`).FindString(src)
	if body == "" {
		t.Fatal("board.ts has no statusOf")
	}
	for _, s := range []string{`"wait"`, `"run"`, `"ok"`, `"fail"`, `"idle"`} {
		if !strings.Contains(body, s) {
			t.Errorf("statusOf does not handle the state %s", s)
		}
	}
	if !strings.Contains(body, `"failed"`) {
		t.Error(`statusOf never gives "failed"`)
	}
	if !strings.Contains(body, `"review"`) {
		t.Error(`statusOf never gives "review" (the review column)`)
	}
}

// A tile shows the step as "3/5", the cost in dollars and the CI state.
func TestPhoneBoardTileFacts(t *testing.T) {
	src := readBoardSrc(t, "src/phone/board.ts")
	for _, want := range []string{
		"export function stepText(",
		"export function costText(",
		"export function tileOf(",
		"`${step}/${steps}`",
		"ci:",
		"project",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("board.ts lost %q", want)
		}
	}
}

// A tap opens the item's own views: a card opens its sheet, a run its
// logs and chat, a lane its diff, logs and chat, a workflow its logs.
func TestPhoneBoardDrillTargets(t *testing.T) {
	src := readBoardSrc(t, "src/phone/board.ts")
	body := regexp.MustCompile(`(?s)export function drillsOf\(.*?\n\}`).FindString(src)
	if body == "" {
		t.Fatal("board.ts has no drillsOf")
	}
	cases := map[string][]string{
		`case "card"`:     {`"card"`},
		`case "run"`:      {`"logs"`, `"chat"`},
		`case "lane"`:     {`"diff"`, `"logs"`, `"chat"`},
		`case "workflow"`: {`"logs"`},
	}
	for c, kinds := range cases {
		i := strings.Index(body, c)
		if i < 0 {
			t.Errorf("drillsOf has no %s", c)
			continue
		}
		rest := body[i+len(c):]
		if j := strings.Index(rest, "case "); j >= 0 {
			rest = rest[:j]
		}
		for _, k := range kinds {
			if !strings.Contains(rest, k) {
				t.Errorf("drillsOf %s does not offer %s", c, k)
			}
		}
	}
}

// The board view: a tab per column with its count, columns that snap
// while the user swipes, tiles from BoardTile, a drill sheet with Cancel
// first, and a refresh no faster than twice a second.
func TestPhoneBoardView(t *testing.T) {
	src := readBoardSrc(t, "src/phone/LiveBoard.tsx")
	for _, want := range []string{
		"export function LiveBoard(",
		`role="tablist"`,
		`role="tab"`,
		"aria-selected=",
		`role="tabpanel"`,
		"<BoardTile",
		"<PhoneSheet",
		"safe: true",
		"scrollIntoView",
		"onScroll=",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("LiveBoard.tsx lost %q", want)
		}
	}
	board := readBoardSrc(t, "src/phone/board.ts")
	m := regexp.MustCompile(`export const BOARD_REFRESH_MS\s*=\s*(\d+)`).FindStringSubmatch(board)
	if m == nil {
		t.Fatal("board.ts has no BOARD_REFRESH_MS")
	}
	if n, _ := strconv.Atoi(m[1]); n < 500 {
		t.Errorf("BOARD_REFRESH_MS = %d; the board asks at most twice a second (≥ 500)", n)
	}
	idx := readBoardSrc(t, "src/phone/index.ts")
	if !strings.Contains(idx, "LiveBoard") {
		t.Error("src/phone/index.ts does not export LiveBoard")
	}
	kit := readBoardSrc(t, "src/kit/main.tsx")
	if !strings.Contains(kit, "<LiveBoard") {
		t.Error("the kit does not show <LiveBoard>")
	}
}

// Columns snap on swipe, tabs are 44 px targets, and the smooth scroll
// stops under reduced motion.
func TestPhoneBoardCSS(t *testing.T) {
	css := readPhoneCSS(t)
	if b := ruleBody(css, ".ph-board-cols"); !strings.Contains(b, "scroll-snap-type: x mandatory") {
		t.Error(".ph-board-cols has no scroll-snap-type: x mandatory")
	}
	if b := ruleBody(css, ".ph-board-col"); !strings.Contains(b, "scroll-snap-align: start") {
		t.Error(".ph-board-col has no scroll-snap-align: start")
	}
	if b := ruleBody(css, ".ph-board-tab"); !strings.Contains(b, "min-height: var(--p-tap)") {
		t.Error(".ph-board-tab has no min-height: var(--p-tap) (44 px target)")
	}
	if !regexp.MustCompile(`(?s)prefers-reduced-motion: reduce\)\s*\{[^@]*\.ph-board-cols\s*\{\s*scroll-behavior: auto`).MatchString(css) {
		t.Error("reduced motion does not turn off the board's smooth scroll")
	}
}

// The board's words are English only (owner 2026-11-07): they are in
// strings.ts and not in the Persian file.
func TestPhoneBoardStringsEnglishOnly(t *testing.T) {
	keys := map[string]bool{}
	for _, e := range allEnglishStrings(t) {
		keys[e.key] = true
	}
	for _, k := range []string{"chips.board", "ph.board.needs_you", "board.col.working", "ph.race.review", "board.col.done",
		"ph.board.empty", "ph.board.today", "ph.board.logs", "ph.board.diff", "ph.stream", "ph.board.card", "ph.board.stale"} {
		if !keys[k] {
			t.Errorf("strings.ts / strings.phone.ts has no %s", k)
		}
	}
	if strings.Contains(readBoardSrc(t, "src/strings.fa.ts"), `"ph.board`) {
		t.Error("strings.fa.ts has phone board strings; the phone app is English only")
	}
}
