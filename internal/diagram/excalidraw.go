package diagram

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/mattn/go-runewidth"
)

type exElement struct {
	ID          string      `json:"id"`
	Type        string      `json:"type"`
	X           float64     `json:"x"`
	Y           float64     `json:"y"`
	Width       float64     `json:"width"`
	Height      float64     `json:"height"`
	Text        string      `json:"text"`
	Points      [][]float64 `json:"points"`
	IsDeleted   bool        `json:"isDeleted"`
	ContainerID string      `json:"containerId"`
}

// excalidraw draws an Excalidraw scene (its JSON): rectangles, ellipses
// and diamonds as boxes, arrows and lines as box-drawing paths, and text
// (centred in its container when it has one). Free drawing and images
// are left out. The scene is scaled to fit width (a cell is about 8×16
// pixels when there is room).
func excalidraw(src string, width int) ([]string, bool) {
	var doc struct {
		Elements []exElement `json:"elements"`
	}
	if err := json.Unmarshal([]byte(src), &doc); err != nil {
		var els []exElement // a bare element list
		if json.Unmarshal([]byte(src), &els) != nil {
			return nil, false
		}
		doc.Elements = els
	}
	var els []exElement
	byID := map[string]exElement{}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	grow := func(x, y float64) {
		if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
			return
		}
		minX, minY, maxX, maxY = math.Min(minX, x), math.Min(minY, y), math.Max(maxX, x), math.Max(maxY, y)
	}
	for _, e := range doc.Elements {
		if e.IsDeleted {
			continue
		}
		switch e.Type {
		case "rectangle", "ellipse", "diamond", "text", "arrow", "line":
		default:
			continue
		}
		els = append(els, e)
		byID[e.ID] = e
		if len(els) > 4*maxNodes {
			return nil, false
		}
		if len(e.Points) > 0 {
			for _, p := range e.Points {
				if len(p) >= 2 {
					grow(e.X+p[0], e.Y+p[1])
				}
			}
			continue
		}
		grow(e.X, e.Y)
		grow(e.X+e.Width, e.Y+e.Height)
	}
	if len(els) == 0 || math.IsInf(minX, 1) {
		return nil, false
	}
	sx := 8.0
	if span := maxX - minX; span/sx+1 > float64(width) {
		sx = span / float64(width-1)
	}
	sy := sx * 2
	cols := int(math.Round((maxX-minX)/sx)) + 1
	rows := int(math.Round((maxY-minY)/sy)) + 1
	if cols > width {
		cols = width
	}
	if (maxY-minY)/sy > maxRows || rows < 1 || cols < 1 || rows > maxRows {
		return nil, false
	}
	at := func(x, y float64) (int, int) {
		cx := int(math.Round((x - minX) / sx))
		cy := int(math.Round((y - minY) / sy))
		return min(max(cx, 0), cols-1), min(max(cy, 0), rows-1)
	}
	c := newCanvas(cols, rows)
	frame := func(e exElement) (x1, y1, x2, y2 int) {
		x1, y1 = at(e.X, e.Y)
		x2, y2 = at(e.X+e.Width, e.Y+e.Height)
		return x1, y1, max(x2, x1+2), max(y2, y1+2)
	}
	// Shapes first (lines join where they meet), then arrows, then text.
	for _, e := range els {
		switch e.Type {
		case "rectangle", "ellipse", "diamond":
			x1, y1, x2, y2 := frame(e)
			c.hline(y1, x1, x2)
			c.hline(y2, x1, x2)
			c.vline(x1, y1, y2)
			c.vline(x2, y1, y2)
			switch e.Type {
			case "ellipse":
				c.set(x1, y1, '╭')
				c.set(x2, y1, '╮')
				c.set(x1, y2, '╰')
				c.set(x2, y2, '╯')
			case "diamond":
				c.set(x1, y1, '╱')
				c.set(x2, y1, '╲')
				c.set(x1, y2, '╲')
				c.set(x2, y2, '╱')
			}
		}
	}
	for _, e := range els {
		if (e.Type != "arrow" && e.Type != "line") || len(e.Points) < 2 {
			continue
		}
		var px, py int
		var dx, dy int
		for k, p := range e.Points {
			if len(p) < 2 {
				continue
			}
			x, y := at(e.X+p[0], e.Y+p[1])
			if k > 0 {
				c.line(px, py, x, y)
				dx, dy = x-px, y-py
				if dx != 0 && dy != 0 { // the elbow ends going down or up
					dx = 0
				}
			}
			px, py = x, y
		}
		if e.Type == "arrow" {
			head := '▶'
			switch {
			case dx < 0:
				head = '◀'
			case dy > 0:
				head = '▼'
			case dy < 0:
				head = '▲'
			}
			if dx != 0 || dy != 0 {
				c.set(px, py, head)
			}
		}
	}
	for _, e := range els {
		if e.Type != "text" {
			continue
		}
		lines := strings.Split(cleanTextKeepNL(e.Text), "\n")
		if box, ok := byID[e.ContainerID]; ok && e.ContainerID != "" {
			x1, y1, x2, y2 := frame(box)
			inner := x2 - x1 - 1
			top := y1 + 1 + max((y2-y1-1-len(lines))/2, 0)
			for j, l := range lines {
				if top+j >= y2 {
					break
				}
				l = cut(strings.TrimSpace(l), inner)
				c.text(x1+1+max((inner-runewidth.StringWidth(l))/2, 0), top+j, l)
			}
			continue
		}
		x, y := at(e.X, e.Y)
		for j, l := range lines {
			c.text(x, y+j, cut(l, cols-x))
		}
	}
	return c.rows(), true
}

func cleanTextKeepNL(s string) string {
	parts := strings.Split(s, "\n")
	for i, p := range parts {
		parts[i] = cleanText(p)
	}
	return strings.Join(parts, "\n")
}
