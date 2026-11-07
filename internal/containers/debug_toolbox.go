package containers

import (
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/services"
)

// DefaultDebugImage is the toolbox image when the user names none: a
// small image with a shell and the usual network and process tools.
const DefaultDebugImage = "busybox:stable"

// DebugToolbox is a visible terminal job that starts a separate,
// throw-away container sharing the process and network namespaces of
// the container id (CTX-43). The target is never changed: nothing is
// copied into it, nothing runs inside it, and the toolbox is removed
// when its shell ends (--rm). image "" means DefaultDebugImage.
func (e *Engine) DebugToolbox(id, image string) (services.Command, error) {
	if image == "" {
		image = DefaultDebugImage
	}
	if id == "" || strings.HasPrefix(id, "-") || strings.ContainsAny(id, " \t\n") {
		return services.Command{}, fmt.Errorf("not a container name: %q", id)
	}
	if strings.HasPrefix(image, "-") || strings.ContainsAny(image, " \t\n") {
		return services.Command{}, fmt.Errorf("not an image name: %q", image)
	}
	argv := []string{e.Binary, "run", "--rm", "-it",
		"--pid", "container:" + id, "--network", "container:" + id,
		"--label", "termocode.debug=" + id,
		image, "sh"}
	return services.Command{Label: "debug: " + id, Argv: argv}, nil
}
