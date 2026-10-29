package shotkit

import (
	"fmt"
	"time"
)

// kubeScene is the Kubernetes view with a canned cluster (app kubeDemo):
// variant "" the lists, "restart" the restart card, "down" a cluster that
// does not answer, "keys" the ? card.
func kubeScene(name, variant string) Scene {
	return Scene{Name: name, Steps: WithEditor(Act("kubedemo:"+variant), Pause(400*time.Millisecond))}
}

// kubeCases are the Kubernetes view (G-K8S) at 150x42 and 80x24, and the
// lists with ASCII icons.
func kubeCases() []goldenCase {
	var out []goldenCase
	for _, sc := range []Scene{kubeScene("kubernetes", ""), kubeScene("kubernetes-restart-confirm", "restart"),
		kubeScene("kubernetes-down", "down"), kubeScene("kubernetes-keys", "keys")} {
		for _, sz := range [][2]int{{150, 42}, {80, 24}} {
			out = append(out, goldenCase{fmt.Sprintf("%s-%dx%d", sc.Name, sz[0], sz[1]), sc, sz[0], sz[1]})
		}
	}
	ascii := kubeScene("kubernetes-ascii", "")
	ascii.Icons = "ascii"
	return append(out, goldenCase{"kubernetes-ascii-150x42", ascii, 150, 42})
}
