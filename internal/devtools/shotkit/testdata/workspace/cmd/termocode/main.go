// Command termocode is the demo entry point.
package main

import (
	"fmt"
	"os"

	"example.com/termocode/internal/statusbar"
)

func main() {
	bar := statusbar.New(80)
	bar.SetBranch("main")
	fmt.Fprintln(os.Stdout, bar.View())
}
