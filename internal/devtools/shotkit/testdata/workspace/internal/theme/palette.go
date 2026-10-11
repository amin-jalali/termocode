// Package theme holds the colours of the demo app.
package theme

// Color is a "#rrggbb" hex string.
type Color string

// The palette.
const (
	BgEditor    Color = "#1e1e1e"
	BgSidebar   Color = "#252526"
	TextPrimary Color = "#d4d4d4"
	TextDim     Color = "#808080"
	Accent      Color = "#007acc"
	Success     Color = "#89d185"
	Warning     Color = "#cca700"
	Danger      Color = "#f14c4c"
)

// Gradient returns n colours from Accent to Success.
func Gradient(n int) []Color {
	out := make([]Color, 0, n)
	for i := 0; i < n; i++ {
		if i < n/2 {
			out = append(out, Accent)
		} else {
			out = append(out, Success)
		}
	}
	return out
}
