package app

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/blocks"
)

// Phone views of termocode's tools without a registry view (mobile plan,
// MOB-11): web_search, web_fetch, workflow_draft, agent_race_start,
// tests_results and db_query (list_dir, search and todo_read live in
// internal/ai). Each one reads only the call's args and the text the
// model got, so the phone never sees more than the model: passwords,
// keys and secret files are already gone from it. A view that cannot
// read its text returns nil and the chat keeps the text only.

// toolBlock is a block of a tool view; the agent fills its id and source.
func toolBlock(kind, title string) *blocks.Block {
	return &blocks.Block{Kind: kind, Title: ai.ViewText(title, blocks.MaxTitle)}
}

func toolField(label, value, kind, tone string) blocks.BlockField {
	return blocks.BlockField{Label: label, Value: ai.ViewText(value, blocks.MaxSub), Kind: kind, Tone: tone}
}

// webSearchView: one row per hit (title, url below, snippet as a second
// line of the sub). The text is "1. title\n   url\n   snippet".
func webSearchView(args map[string]any, out string) *blocks.Block {
	q := ai.ArgString(args, "query")
	if strings.TrimSpace(out) == "No results." {
		b := toolBlock(blocks.KindStatus, "No web results for "+strconv.Quote(q))
		b.State = blocks.ToneIdle
		return ai.ViewDone(b)
	}
	var items []blocks.BlockItem
	var cur *blocks.BlockItem
	var lines []string
	flush := func() {
		if cur == nil {
			return
		}
		if len(lines) > 0 {
			cur.Badge = webHost(lines[0])
		}
		cur.Sub = ai.ViewText(strings.Join(lines, " · "), blocks.MaxSub)
		items = append(items, *cur)
		cur, lines = nil, nil
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "   ") {
			if cur != nil {
				lines = append(lines, strings.TrimSpace(l))
			}
			continue
		}
		num, title, ok := strings.Cut(l, ". ")
		if _, err := strconv.Atoi(num); !ok || err != nil {
			continue
		}
		flush()
		if len(items) == blocks.MaxItems {
			break
		}
		t := ai.ViewText(title, blocks.MaxTitle)
		if t == "" {
			t = "(no title)"
		}
		cur = &blocks.BlockItem{ID: ai.ViewItemID(len(items)), Title: t}
	}
	flush()
	if len(items) == 0 {
		return nil
	}
	b := toolBlock(blocks.KindList, "Web: "+strconv.Quote(q)+" · "+strconv.Itoa(len(items)))
	b.Items = items
	return ai.ViewDone(b)
}

// webHost is the host of a url, cut to a badge ("" when it is not one).
func webHost(u string) string {
	rest, ok := strings.CutPrefix(u, "https://")
	if !ok {
		if rest, ok = strings.CutPrefix(u, "http://"); !ok {
			return ""
		}
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	rest = strings.TrimPrefix(rest, "www.")
	if len(rest) > blocks.MaxID {
		return ""
	}
	return ai.ViewText(rest, blocks.MaxID)
}

// webFetchView: a detail block with the page's title, url, type and the
// start of its text. The text is "title\nurl (type[, cut])\n---\ntext".
func webFetchView(_ map[string]any, out string) *blocks.Block {
	head, body, ok := strings.Cut(out, "\n---\n")
	if !ok {
		return nil
	}
	hl := strings.Split(head, "\n")
	title, info := "", hl[len(hl)-1]
	if len(hl) > 1 {
		title = hl[0]
	}
	i := strings.LastIndex(info, " (")
	if i < 0 || !strings.HasSuffix(info, ")") {
		return nil
	}
	u, typ := info[:i], strings.TrimSuffix(info[i+2:], ")")
	typ, cut := strings.CutSuffix(typ, ", cut")
	if title == "" {
		title = webHost(u)
	}
	if title == "" {
		title = "Web page"
	}
	b := toolBlock(blocks.KindDetail, title)
	b.Fields = []blocks.BlockField{toolField("URL", u, blocks.ColMono, ""), toolField("Type", typ, blocks.ColText, "")}
	if cut {
		b.Fields = append(b.Fields, toolField("Length", "cut (the page was longer than the limit)", blocks.ColText, blocks.ToneWarn))
	}
	if t := ai.ViewText(body, blocks.MaxSub); t != "" {
		b.Fields = append(b.Fields, toolField("Text", t, blocks.ColText, ""))
	}
	return ai.ViewDone(b)
}

var workflowCountsRe = regexp.MustCompile(`\(valid: (\d+) step\(s\), (\d+) write\(s\)`)

// workflowDraftView: a card of the saved workflow file (name, file, steps,
// writes, trusted). Nothing ran; the card says so.
func workflowDraftView(args map[string]any, out string) *blocks.Block {
	name := ai.ArgString(args, "name")
	verb, rest := "Created", ""
	if r, ok := strings.CutPrefix(out, "Created "); ok {
		rest = r
	} else if r, ok := strings.CutPrefix(out, "Saved the change to "); ok {
		verb, rest = "Saved", r
	} else {
		return nil
	}
	file, _, _ := strings.Cut(rest, " (valid")
	m := workflowCountsRe.FindStringSubmatch(out)
	if m == nil || name == "" {
		return nil
	}
	b := toolBlock(blocks.KindCard, verb+" workflow "+name)
	b.Fields = []blocks.BlockField{
		toolField("File", file, blocks.ColMono, ""),
		toolField("Steps", m[1], blocks.ColNum, ""),
		toolField("Writes", m[2], blocks.ColNum, ""),
	}
	if strings.Contains(out, "; trusted for this version") {
		b.Fields = append(b.Fields, toolField("Trust", "trusted for this version", blocks.ColTone, blocks.ToneOK))
	}
	b.Fields = append(b.Fields, toolField("Ran", "nothing ran", blocks.ColTone, blocks.ToneIdle))
	if id := ai.ViewText(name, blocks.MaxTitle); id != "" {
		b.Ref = &blocks.BlockRef{Kind: "workflow", ID: id}
	}
	if p := ai.ViewPath(file); p != "" && b.Ref != nil {
		b.Ref.Path = p
	}
	return ai.ViewDone(b)
}

// raceStartView: a card of the race the user approved (task, lanes,
// verify), drawn busy: the lanes are working now.
func raceStartView(args map[string]any, out string) *blocks.Block {
	if !strings.HasPrefix(out, "Race started") {
		return nil
	}
	var lanes []string
	if as, ok := args["agents"].([]any); ok {
		for _, a := range as {
			if s, ok := a.(string); ok && s != "" {
				lanes = append(lanes, s)
			}
		}
	}
	title := "Race started"
	if len(lanes) > 0 {
		title += " · " + strconv.Itoa(len(lanes)) + " lanes"
	}
	b := toolBlock(blocks.KindCard, title)
	b.Fields = []blocks.BlockField{toolField("State", "working", blocks.ColTone, blocks.ToneBusy)}
	if t := ai.ArgString(args, "task"); t != "" {
		b.Fields = append(b.Fields, toolField("Task", t, blocks.ColText, ""))
	}
	if len(lanes) > 0 {
		b.Fields = append(b.Fields, toolField("Lanes", strings.Join(lanes, ", "), blocks.ColText, ""))
	}
	if v := ai.ArgString(args, "verify"); v != "" {
		b.Fields = append(b.Fields, toolField("Verify", v, blocks.ColMono, ""))
	}
	b.Ref = &blocks.BlockRef{Kind: "race"}
	return ai.ViewDone(b)
}

var (
	testsCountsRe = regexp.MustCompile(`(\d+) passed, (\d+) failed, (\d+) skipped\.`)
	testsFailRe   = regexp.MustCompile(`^- (.+?) \(([^()]*):(\d+)\)(?:: (.*))?$`)
	testsPkgRe    = regexp.MustCompile(`^- (.+?) \(([^()]*)\): (.*)$`)
)

// testsResultsView: a status line when nothing failed (✓ passed, ◐ still
// running), else a list of the failures (file:line, message) and package
// errors, each referring to its file and line.
func testsResultsView(_ map[string]any, out string) *blocks.Block {
	lines := strings.Split(out, "\n")
	m := testsCountsRe.FindStringSubmatch(lines[0])
	if m == nil {
		return nil
	}
	passed, failed, skipped := m[1], m[2], m[3]
	running := strings.HasPrefix(lines[0], "Tests are")
	sum := passed + " passed, " + failed + " failed, " + skipped + " skipped"
	var items []blocks.BlockItem
	more := 0
	section := ""
	for _, l := range lines[1:] {
		switch {
		case strings.HasSuffix(l, " failed:"):
			section = "fail"
			continue
		case strings.HasPrefix(l, "Package errors"):
			section = "pkg"
			continue
		case strings.HasPrefix(l, "    ") && len(items) > 0:
			it := &items[len(items)-1]
			it.Sub = ai.ViewText(it.Sub+" "+strings.TrimSpace(l), blocks.MaxSub)
			continue
		case strings.HasPrefix(l, "… and "):
			if n, err := strconv.Atoi(strings.Fields(l)[2]); err == nil {
				more += n
			}
			continue
		case !strings.HasPrefix(l, "- "):
			section = ""
			continue
		}
		var it blocks.BlockItem
		switch section {
		case "fail":
			f := testsFailRe.FindStringSubmatch(l)
			if f == nil {
				continue
			}
			it = blocks.BlockItem{Title: ai.ViewText(f[1], blocks.MaxTitle), Sub: ai.ViewText(f[4], blocks.MaxSub),
				Tone: blocks.ToneBad, Badge: ai.ViewText(lastSeg(f[2])+":"+f[3], blocks.MaxID)}
			line, _ := strconv.Atoi(f[3])
			if p := ai.ViewPath(f[2]); p != "" {
				it.Ref = &blocks.BlockRef{Kind: "lines", Path: p, Line: line}
			}
		case "pkg":
			f := testsPkgRe.FindStringSubmatch(l)
			if f == nil {
				continue
			}
			it = blocks.BlockItem{Title: ai.ViewText(f[1], blocks.MaxTitle), Sub: ai.ViewText(f[3], blocks.MaxSub),
				Tone: blocks.ToneBad, Badge: "build"}
			loc, num, _ := strings.Cut(f[2], ":")
			line, _ := strconv.Atoi(num)
			if p := ai.ViewPath(loc); p != "" {
				it.Ref = &blocks.BlockRef{Kind: "lines", Path: p, Line: line}
			}
		default:
			continue
		}
		if len(items) == blocks.MaxItems {
			more++
			continue
		}
		if it.Title == "" {
			it.Title = "(no name)"
		}
		it.ID = ai.ViewItemID(len(items))
		items = append(items, it)
	}
	if len(items) == 0 {
		b := toolBlock(blocks.KindStatus, "Tests: "+sum)
		switch {
		case running:
			b.State = blocks.ToneBusy
			b.Title = ai.ViewText("Tests running: "+sum+" so far", blocks.MaxTitle)
		case failed != "0":
			b.State = blocks.ToneBad
		default:
			b.State = blocks.ToneOK
		}
		return ai.ViewDone(b)
	}
	title := "Tests: " + sum
	if running {
		title = "Tests running: " + sum + " so far"
	}
	b := toolBlock(blocks.KindList, title)
	b.Items = items
	if more > 0 {
		b.Total, b.Truncated = len(items)+more, true
	}
	return ai.ViewDone(b)
}

// lastSeg is the last path segment of p.
func lastSeg(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// dbQueryView: a table of the rows (≤ 12 columns, ≤ 200 rows), or a status
// line for a statement without rows. The text is "N rows on conn · X ms",
// then "col | col" and one "cell | cell" line per row (a | in a cell is
// \|), then the truncated and notice lines.
func dbQueryView(_ map[string]any, out string) *blocks.Block {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	head := lines[0]
	if !strings.Contains(head, " on ") || !strings.HasSuffix(head, " ms") {
		return nil
	}
	var body, notices []string
	cut := false
	for _, l := range lines[1:] {
		switch {
		case strings.HasPrefix(l, "… [truncated"):
			cut = true
		case strings.HasPrefix(l, "notice: "):
			notices = append(notices, strings.TrimPrefix(l, "notice: "))
		default:
			body = append(body, l)
		}
	}
	if len(body) == 0 {
		b := toolBlock(blocks.KindStatus, head)
		b.State = blocks.ToneOK
		if len(notices) > 0 {
			b.ForYou = ai.ViewText("notice: "+strings.Join(notices, " · "), blocks.MaxForYou)
		}
		return ai.ViewDone(b)
	}
	names := dbViewCells(body[0])
	if len(names) == 0 {
		return nil
	}
	ncol := min(len(names), blocks.MaxColumns)
	b := toolBlock(blocks.KindTable, head)
	for i, n := range names[:ncol] {
		label := ai.ViewText(n, blocks.MaxTitle)
		b.Columns = append(b.Columns, blocks.BlockColumn{ID: "c" + strconv.Itoa(i), Label: label, Kind: blocks.ColText})
	}
	for _, l := range body[1:] {
		cells := dbViewCells(l)
		if len(cells) != len(names) {
			return nil // a line we cannot split: text only
		}
		if len(b.Rows) == blocks.MaxRows {
			cut = true
			break
		}
		row := make([]string, ncol)
		for i := range row {
			row[i] = ai.ViewText(cells[i], blocks.MaxSub)
		}
		b.Rows = append(b.Rows, row)
	}
	if n, _, ok := strings.Cut(head, " row"); ok {
		if t, err := strconv.Atoi(n); err == nil && t > len(b.Rows) {
			b.Total = t
		}
	}
	b.Truncated = cut || b.Total > len(b.Rows) || ncol < len(names)
	if ncol < len(names) {
		b.ForYou = ai.ViewText("showing "+strconv.Itoa(ncol)+" of "+strconv.Itoa(len(names))+" columns", blocks.MaxForYou)
	}
	if len(notices) > 0 {
		n := "notice: " + strings.Join(notices, " · ")
		if b.ForYou != "" {
			n = b.ForYou + " · " + n
		}
		b.ForYou = ai.ViewText(n, blocks.MaxForYou)
	}
	return ai.ViewDone(b)
}

// dbViewCells splits one "a | b" line of dbAIResultText into its cells,
// turning \| back into |.
func dbViewCells(l string) []string {
	parts := strings.Split(l, " | ")
	for i, p := range parts {
		parts[i] = strings.ReplaceAll(p, `\|`, "|")
	}
	return parts
}
