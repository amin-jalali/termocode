package shotkit

import "time"

// KeyHints is the key hint popup (app keyHintsDemo): arg "" = after the
// leader key, "group" = one level deeper, "notice" = the "no binding" hint.
func KeyHints(name, arg string) Scene {
	return Scene{Name: name, Steps: WithEditor(Act("keyhintdemo:"+arg), Pause(300*time.Millisecond))}
}
