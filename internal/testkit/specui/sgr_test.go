package specui

import "testing"

func TestApplySGR(t *testing.T) {
	fg, bg := applySGR("38;2;197;134;192;48;2;28;28;28", "", "")
	if fg != "#c586c0" || bg != "#1c1c1c" {
		t.Errorf("fg=%s bg=%s", fg, bg)
	}
	fg, bg = applySGR("1;38;2;135;135;135;48;2;58;58;58", "", "")
	if fg != "#878787" || bg != "#3a3a3a" {
		t.Errorf("fg=%s bg=%s", fg, bg)
	}
}
