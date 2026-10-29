package shotkit

import "fmt"

// keyHintCases are the key hint popup frames at the two sizes the popup is
// checked at: 150x42 (a box at the bottom right) and 80x24 (full width).
func keyHintCases() []goldenCase {
	var out []goldenCase
	for _, sc := range []Scene{KeyHints("key-hints", ""), KeyHints("key-hints-group", "group"), KeyHints("key-hints-notice", "notice")} {
		for _, sz := range [][2]int{{150, 42}, {80, 24}} {
			out = append(out, goldenCase{fmt.Sprintf("%s-%dx%d", sc.Name, sz[0], sz[1]), sc, sz[0], sz[1]})
		}
	}
	return out
}
