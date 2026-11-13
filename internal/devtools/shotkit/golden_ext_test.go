package shotkit

import (
	"fmt"
	"time"
)

// extPanelLua adds a sidebar panel with its own activity-bar icon.
const extPanelLua = `local tc = termocode
tc.register_panel({
  id = 'main',
  title = 'NOTES',
  icon = 'Q',
  render = function(width, height)
    return { '{{accent bold}}NOTES{{/}}', '', 'Two notes today.' }
  end,
})
`

// extBrokenLua fails while it loads (SCN-29).
const extBrokenLua = `error('boom: this extension is broken')
`

// extCases are the extension screens (SCN-29): an extension that adds a
// sidebar icon, and the same next to one that crashes at load (the icon
// stays and the editor is usable). Each runs at 150x42 and 80x24.
func extCases() []goldenCase {
	var out []goldenCase
	scenes := []Scene{
		{Name: "ext-panel-icon", Extensions: map[string]string{"notes": extPanelLua},
			Steps: WithEditor(Pause(1500 * time.Millisecond))},
		{Name: "ext-crashing-beside-icon", Extensions: map[string]string{"notes": extPanelLua, "broken": extBrokenLua},
			Steps: WithEditor(Pause(1500 * time.Millisecond))},
	}
	for _, sc := range scenes {
		for _, sz := range [][2]int{{150, 42}, {80, 24}} {
			out = append(out, goldenCase{fmt.Sprintf("%s-%dx%d", sc.Name, sz[0], sz[1]), sc, sz[0], sz[1]})
		}
	}
	return out
}
