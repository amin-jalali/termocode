package keyhint

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

const (
	narrowBelow = 100 // narrower screens get a full-width popup
	maxBoxW     = 76  // widest popup on a wide screen
	maxDescW    = 30
)

// KeyLabel is a key token as people read it: "<space>" is "Space".
func KeyLabel(tok string) string {
	if len(tok) > 2 && tok[0] == '<' && tok[len(tok)-1] == '>' {
		n := tok[1 : len(tok)-1]
		switch {
		case n == "cr":
			return "Enter"
		case n == "bs":
			return "Backspace"
		case strings.HasPrefix(n, "c-") && len(n) == 3:
			return "Ctrl+" + strings.ToUpper(n[2:])
		case strings.HasPrefix(n, "a-") || strings.HasPrefix(n, "m-"):
			return "Alt+" + n[2:]
		}
		return strings.ToUpper(n[:1]) + n[1:]
	}
	return tok
}

func pad(s string, w int) string {
	if d := w - ansi.StringWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// Layout draws the popup for a w x h screen. It returns the rows (each
// as wide as the box) and the cell of the top-left corner. The box sits
// at the bottom right, above the status bar; on a narrow screen it spans
// the full width. ok is false when the screen is too small.
func Layout(c Content, w, h int) (rows []string, x, y int, ok bool) {
	if w < 20 || h < 5 {
		return nil, 0, 0, false
	}
	f := theme.Float()
	bg := lipgloss.NewStyle().Background(f.Bg)
	border := bg.Foreground(f.Border)
	boxW := min(w, maxBoxW)
	if w < narrowBelow {
		boxW = w
	}
	inner := boxW - 4 // border and one space of padding each side

	var body []string
	if c.Notice != "" {
		body = []string{bg.Foreground(f.Danger).Render(pad(" "+c.Notice, inner))}
	} else {
		keyW, descW := 1, 1
		cells := make([][2]string, len(c.Entries))
		for i, e := range c.Entries {
			k, d := KeyLabel(e.Key), e.Desc
			if e.Group && !strings.HasPrefix(d, "+") {
				d = "+" + d
			}
			cells[i] = [2]string{k, ansi.Truncate(d, maxDescW, "…")}
			keyW = max(keyW, ansi.StringWidth(k))
			descW = max(descW, ansi.StringWidth(cells[i][1]))
		}
		cellW := keyW + 2 + descW
		cols := max(1, (inner+2)/(cellW+2))
		if cellW > inner {
			cellW, descW = inner, max(1, inner-keyW-2)
		}
		maxRows := max(1, h-5)
		n := len(cells)
		perCol := (n + cols - 1) / cols
		if perCol > maxRows {
			perCol = maxRows
			if cols*perCol < n {
				cells = cells[:cols*perCol-1]
				cells = append(cells, [2]string{"…", "more"})
				n = len(cells)
			}
		}
		for r := 0; r < perCol; r++ {
			var line strings.Builder
			for col := 0; col < cols; col++ {
				i := col*perCol + r
				if i >= n {
					break
				}
				if col > 0 {
					line.WriteString(bg.Render("  "))
				}
				k, d := cells[i][0], ansi.Truncate(cells[i][1], descW, "…")
				line.WriteString(bg.Foreground(f.Primary).Bold(true).Render(pad(k, keyW)))
				line.WriteString(bg.Foreground(f.Hint).Render("  "))
				line.WriteString(bg.Foreground(f.Text).Render(pad(d, descW)))
			}
			body = append(body, padTo(line.String(), inner, bg))
		}
	}

	title := " " + c.Title + " "
	if c.Notice != "" {
		title = " Key hints "
	} else {
		parts := strings.Fields(c.Title)
		for i, p := range parts {
			parts[i] = KeyLabel(p)
		}
		title = " " + strings.Join(parts, " ") + " "
	}
	title = ansi.Truncate(title, boxW-4, "")
	top := border.Render("╭") + bg.Foreground(f.Title).Bold(true).Render(title) +
		border.Render(strings.Repeat("─", boxW-2-ansi.StringWidth(title))+"╮")
	rows = append(rows, top)
	for _, l := range body {
		rows = append(rows, border.Render("│")+bg.Render(" ")+padTo(l, inner, bg)+bg.Render(" ")+border.Render("│"))
	}
	rows = append(rows, border.Render("╰"+strings.Repeat("─", boxW-2)+"╯"))
	if len(rows) > h-1 {
		return nil, 0, 0, false
	}
	return rows, w - boxW, h - 1 - len(rows), true
}

// padTo pads a styled string with background blanks to width w.
func padTo(s string, w int, bg lipgloss.Style) string {
	if d := w - ansi.StringWidth(s); d > 0 {
		return s + bg.Render(strings.Repeat(" ", d))
	}
	return s
}
