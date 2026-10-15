package tea

// ParseInput decodes b as one complete burst of terminal input into the
// messages a Program would get (keys, mouse, focus, paste). termocode
// patch: lets tests check that every shortcut's bytes reach the app.
func ParseInput(b []byte) []Msg {
	var out []Msg
	for i := 0; i < len(b); {
		w, msg := detectOneMsg(b[i:], false)
		if w <= 0 {
			break
		}
		out = append(out, msg)
		i += w
	}
	return out
}
