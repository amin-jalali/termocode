package git

// Lanes for a drawn commit graph (the web's Git Graph view): each commit
// gets a column, and each row says which lines cross its top half (from
// the row above into the node or straight through) and its bottom half
// (from the node to its parents' lanes, and the lanes passing by). The
// page only draws what this says.

// GraphEdge is one line in half a row: from a column at the half's top
// to a column at its bottom. Color is the lane's colour number (the page
// takes it modulo its palette).
type GraphEdge struct {
	From, To, Color int
}

// GraphRow is the drawing of one commit's row.
type GraphRow struct {
	Col   int         // the node's column
	Color int         // the node's lane colour
	Up    []GraphEdge // top half: row edge (From) → middle (To)
	Down  []GraphEdge // bottom half: middle (From) → row edge (To)
}

type graphLane struct {
	hash  string // the commit the lane waits for ("" : free)
	color int
}

// GraphLanes lays out cs (newest first, each parent listed after its
// children, as git log gives them). A parent that is not in cs keeps its
// lane going out of the last row.
func GraphLanes(cs []GraphCommit) []GraphRow {
	rows := make([]GraphRow, 0, len(cs))
	var lanes []graphLane
	next := 0
	free := func() int {
		for i, l := range lanes {
			if l.hash == "" {
				return i
			}
		}
		lanes = append(lanes, graphLane{})
		return len(lanes) - 1
	}
	for _, c := range cs {
		col := -1
		var r GraphRow
		for i, l := range lanes {
			if l.hash == "" {
				continue
			}
			to := i
			if l.hash == c.Hash {
				if col < 0 {
					col = i
				}
				to = col
			}
			r.Up = append(r.Up, GraphEdge{From: i, To: to, Color: l.color})
		}
		if col < 0 {
			col = free()
			lanes[col] = graphLane{color: next}
			next++
		}
		r.Col, r.Color = col, lanes[col].color
		for i := range lanes {
			if lanes[i].hash == c.Hash {
				lanes[i].hash = ""
			}
		}
		mine := map[int]bool{}
		for k, p := range c.Parents {
			if k == 0 {
				lanes[col] = graphLane{hash: p, color: r.Color}
				mine[col] = true
				continue
			}
			at := -1
			for i, l := range lanes {
				if l.hash == p {
					at = i
					break
				}
			}
			if at < 0 {
				at = free()
				lanes[at] = graphLane{hash: p, color: next}
				next++
			}
			mine[at] = true
		}
		for i, l := range lanes {
			switch {
			case l.hash == "":
			case mine[i]:
				r.Down = append(r.Down, GraphEdge{From: col, To: i, Color: l.color})
			default:
				r.Down = append(r.Down, GraphEdge{From: i, To: i, Color: l.color})
			}
		}
		for len(lanes) > 0 && lanes[len(lanes)-1].hash == "" {
			lanes = lanes[:len(lanes)-1]
		}
		rows = append(rows, r)
	}
	return rows
}
