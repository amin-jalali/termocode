// Command gen writes proto.d.ts and schema.json of internal/webproto.
// Run it with: go generate ./internal/webproto
package main

import (
	"fmt"
	"os"

	"github.com/amin-jalali/termocode/internal/webproto"
)

func main() {
	for _, f := range []struct {
		name string
		gen  func() ([]byte, error)
	}{
		{"proto.d.ts", webproto.GenerateTS},
		{"schema.json", webproto.GenerateSchema},
	} {
		b, err := f.gen()
		if err == nil {
			err = os.WriteFile(f.name, b, 0o644)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "webproto gen:", err)
			os.Exit(1)
		}
	}
}
