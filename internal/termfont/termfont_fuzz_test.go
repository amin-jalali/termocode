package termfont

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// FuzzParseKitty reads any DCS body. An accepted body gives exactly the
// signed whole step it asks for, and that step writes back to itself.
func FuzzParseKitty(f *testing.F) {
	for _, d := range []int{-3, -1, 1, 4} {
		f.Add(strings.TrimSuffix(strings.TrimPrefix(Request(Kitty, d, d > 0), "\x1bP"), "\x1b\\"))
	}
	for _, s := range []string{
		`@kitty-cmd{"cmd":"ls","version":[0,14,2]}`,
		`@kitty-cmd{"cmd":"set-font-size","version":[0,14,2],"payload":{"size":12}}`,
		`@kitty-cmd{"cmd":"set-font-size","payload":{"size":2,"increment_op":"*"}}`,
		`@kitty-cmd{"cmd":"set-font-size","payload":{"size":2.5,"increment_op":"-"}}`,
		`@kitty-cmd{broken`,
		`+q544e`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, body string) {
		d, ok := ParseKitty(body)
		if !ok {
			return
		}
		var c struct {
			Payload struct {
				Size        float64 `json:"size"`
				IncrementOp string  `json:"increment_op"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(body, kittyPrefix)), &c); err != nil {
			t.Fatalf("accepted %q that is not JSON: %v", body, err)
		}
		want := math.Trunc(c.Payload.Size)
		if c.Payload.IncrementOp == "-" {
			want = -want
		}
		if d == 0 || float64(d) != want {
			t.Fatalf("%q: delta %d, want %v", body, d, want)
		}
		back := strings.TrimSuffix(strings.TrimPrefix(Request(Kitty, d, false), "\x1bP"), "\x1b\\")
		if d2, ok := ParseKitty(back); !ok || d2 != d {
			t.Fatalf("delta %d writes back as %d, %v", d, d2, ok)
		}
	})
}
