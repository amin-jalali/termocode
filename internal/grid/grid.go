package grid

// Cell is one cell on the nvim grid: a rune plus a highlight attribute id.
type Cell struct {
	Rune rune
	HlID int
}

// Attrs is the resolved set of attributes nvim defines for one hl id.
// Fg/Bg/Sp are 24-bit RGB values, or -1 for "use default".
type Attrs struct {
	Fg, Bg, Sp    int
	Bold          bool
	Italic        bool
	Underline     bool
	Undercurl     bool
	Strikethrough bool
	Reverse       bool
}

// Grid is the cell buffer plus cursor position and highlight palette.
type Grid struct {
	Width, Height        int
	Cells                [][]Cell
	CursorRow, CursorCol int

	DefaultFg int
	DefaultBg int
	DefaultSp int

	HlAttrs map[int]Attrs
}

// New returns a freshly initialized Grid with default colors set to "use
// terminal default".
func New() *Grid {
	g := &Grid{
		DefaultFg: -1,
		DefaultBg: -1,
		DefaultSp: -1,
		HlAttrs:   map[int]Attrs{0: {Fg: -1, Bg: -1, Sp: -1}},
	}
	return g
}

// Apply consumes a batch of redraw events from nvim and mutates the grid.
// Each event is of the form [name, instance1, instance2, ...] where each
// instance is the args for one occurrence of the event.
func (g *Grid) Apply(events [][]any) {
	for _, ev := range events {
		if len(ev) == 0 {
			continue
		}
		name, _ := ev[0].(string)
		if len(ev) == 1 {
			g.applyOne(name, nil)
			continue
		}
		for _, inst := range ev[1:] {
			args, _ := inst.([]any)
			g.applyOne(name, args)
		}
	}
}

func (g *Grid) applyOne(name string, args []any) {
	switch name {
	case "grid_resize":
		if len(args) < 3 {
			return
		}
		w := toInt(args[1])
		h := toInt(args[2])
		g.resize(w, h)
	case "grid_clear":
		for r := range g.Cells {
			for c := range g.Cells[r] {
				g.Cells[r][c] = Cell{Rune: ' ', HlID: 0}
			}
		}
	case "grid_line":
		if len(args) < 4 {
			return
		}
		row := toInt(args[1])
		colStart := toInt(args[2])
		cellsArr, _ := args[3].([]any)
		g.applyLine(row, colStart, cellsArr)
	case "grid_scroll":
		if len(args) < 7 {
			return
		}
		top := toInt(args[1])
		bot := toInt(args[2])
		left := toInt(args[3])
		right := toInt(args[4])
		rows := toInt(args[5])
		g.scroll(top, bot, left, right, rows)
	case "grid_cursor_goto":
		if len(args) < 3 {
			return
		}
		g.CursorRow = toInt(args[1])
		g.CursorCol = toInt(args[2])
	case "default_colors_set":
		if len(args) < 3 {
			return
		}
		g.DefaultFg = toInt(args[0])
		g.DefaultBg = toInt(args[1])
		g.DefaultSp = toInt(args[2])
	case "hl_attr_define":
		if len(args) < 2 {
			return
		}
		id := toInt(args[0])
		g.HlAttrs[id] = decodeAttrs(args[1])
	}
}

func (g *Grid) resize(w, h int) {
	g.Width = w
	g.Height = h
	cells := make([][]Cell, h)
	for r := range cells {
		cells[r] = make([]Cell, w)
		for c := range cells[r] {
			cells[r][c] = Cell{Rune: ' ', HlID: 0}
		}
	}
	g.Cells = cells
}

func (g *Grid) applyLine(row, colStart int, cellsArr []any) {
	if row < 0 || row >= g.Height {
		return
	}
	col := colStart
	lastHl := 0
	for _, cellAny := range cellsArr {
		cell, _ := cellAny.([]any)
		if len(cell) == 0 {
			continue
		}
		text, _ := cell[0].(string)
		hl := lastHl
		repeat := 1
		if len(cell) >= 2 {
			hl = toInt(cell[1])
		}
		if len(cell) >= 3 {
			repeat = toInt(cell[2])
		}
		lastHl = hl

		var r rune = ' '
		for _, x := range text {
			r = x
			break
		}
		for i := 0; i < repeat; i++ {
			if col >= g.Width {
				break
			}
			g.Cells[row][col] = Cell{Rune: r, HlID: hl}
			col++
		}
	}
}

func (g *Grid) scroll(top, bot, left, right, rows int) {
	if rows == 0 {
		return
	}
	if top < 0 || bot > g.Height || top >= bot {
		return
	}
	if left < 0 || right > g.Width || left >= right {
		return
	}

	if rows > 0 {
		for r := top; r < bot-rows; r++ {
			for c := left; c < right; c++ {
				g.Cells[r][c] = g.Cells[r+rows][c]
			}
		}
	} else {
		for r := bot - 1; r >= top-rows; r-- {
			for c := left; c < right; c++ {
				g.Cells[r][c] = g.Cells[r+rows][c]
			}
		}
	}
}

func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int8:
		return int(x)
	case int16:
		return int(x)
	case int32:
		return int(x)
	case int64:
		return int(x)
	case uint:
		return int(x)
	case uint8:
		return int(x)
	case uint16:
		return int(x)
	case uint32:
		return int(x)
	case uint64:
		return int(x)
	case float64:
		return int(x)
	case float32:
		return int(x)
	}
	return 0
}

func decodeAttrs(v any) Attrs {
	a := Attrs{Fg: -1, Bg: -1, Sp: -1}
	m, ok := v.(map[string]any)
	if !ok {
		mi, ok := v.(map[any]any)
		if !ok {
			return a
		}
		m = make(map[string]any, len(mi))
		for k, val := range mi {
			if ks, ok := k.(string); ok {
				m[ks] = val
			}
		}
	}
	if x, ok := m["foreground"]; ok {
		a.Fg = toInt(x)
	}
	if x, ok := m["background"]; ok {
		a.Bg = toInt(x)
	}
	if x, ok := m["special"]; ok {
		a.Sp = toInt(x)
	}
	if x, ok := m["bold"].(bool); ok {
		a.Bold = x
	}
	if x, ok := m["italic"].(bool); ok {
		a.Italic = x
	}
	if x, ok := m["underline"].(bool); ok {
		a.Underline = x
	}
	if x, ok := m["undercurl"].(bool); ok {
		a.Undercurl = x
	}
	if x, ok := m["strikethrough"].(bool); ok {
		a.Strikethrough = x
	}
	if x, ok := m["reverse"].(bool); ok {
		a.Reverse = x
	}
	return a
}
