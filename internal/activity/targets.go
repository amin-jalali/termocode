package activity

// Item is one icon of the bar and the rows it takes (Y, H).
type Item struct {
	View  View
	Label string // tooltip name, e.g. "Explorer"
	Y, H  int
}

// Items lists the icons on show with their rows, top group first. A
// click on any of an item's rows hits it (see HandleMouse).
func (m Model) Items() []Item {
	var out []Item
	for i, it := range topAll() {
		y := i * itemStride
		if y+itemPitch > m.h {
			break
		}
		out = append(out, Item{View: it.view, Label: it.label, Y: y, H: itemPitch})
	}
	if bs := m.bottomGroupStart(); bs >= 0 {
		for i, it := range bottomItems {
			out = append(out, Item{View: it.view, Label: it.label, Y: bs + i*itemStride, H: itemPitch})
		}
	}
	return out
}

// ItemAt returns the item a click on row y hits, as HandleMouse sees it.
func (m Model) ItemAt(y int) (Item, bool) {
	if y < 0 || y >= m.h {
		return Item{}, false
	}
	v, ok := m.itemAt(y)
	if !ok {
		return Item{}, false
	}
	for _, it := range m.Items() {
		if it.View == v {
			return it, true
		}
	}
	return Item{View: v}, true
}
