package dbclient

import (
	"fmt"
	"math"
	"strings"
)

// PlanBars is the bar view of p (DBX-39): one line per node, a bar of
// width cells for its Share, the percent and the node text.
func PlanBars(p Plan, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, l := range p.Lines {
		if l.Detail {
			continue
		}
		n := int(math.Round(l.Share * float64(width)))
		if n > width {
			n = width
		}
		bar := strings.Repeat("█", n) + strings.Repeat("░", width-n)
		out = append(out, fmt.Sprintf("%s %5.1f%% %s", bar, l.Share*100, l.Text))
	}
	return out
}
