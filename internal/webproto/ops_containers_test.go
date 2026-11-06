package webproto

import (
	"encoding/json"
	"testing"
)

// Spec (WEBP containers): the host's images, networks, volumes, contexts
// and registries, an inspect (env values masked on the hub), an image's
// history, one stats sample and the disk usage are reads for viewers.
func TestContainersHostReadOps(t *testing.T) {
	checkArgs(t, OpContainersHost, nil, []string{`{}`})
	checkRow(t, OpContainersHost, RoleViewer, false, AuditNo)
	checkArgs(t, OpContainersInspect,
		[]string{`{"kind":"pod","id":"web"}`, `{"kind":"container","id":"api-1"}`, `{"kind":"image","id":"-x"}`, `{"kind":"volume","id":"a b"}`, `{"kind":"network","id":""}`},
		[]string{`{"kind":"container","id":"9f3c2e1a1234"}`, `{"kind":"image","id":"ghcr.io/acme/api:1.4"}`, `{"kind":"image","id":"sha256:4b1c9e"}`, `{"kind":"network","id":"shop_default"}`, `{"kind":"volume","id":"pgdata"}`})
	checkRow(t, OpContainersInspect, RoleViewer, false, AuditNo)
	checkArgs(t, OpContainersHistory, []string{`{"ref":""}`, `{"ref":"--all"}`, `{"ref":"a\u001bb"}`}, []string{`{"ref":"nginx:1.27"}`})
	checkRow(t, OpContainersHistory, RoleViewer, false, AuditNo)
	checkArgs(t, OpContainersStats, []string{`{"id":"api-1"}`}, []string{`{"id":"9f3c2e1a1234"}`})
	checkRow(t, OpContainersStats, RoleViewer, false, AuditNo)
	checkArgs(t, OpContainersDiskUsage, nil, []string{`{}`})
	checkRow(t, OpContainersDiskUsage, RoleViewer, false, AuditNo)
}

// Spec: a Docker Hub search and a remote repository's tags ask the
// network on the user's behalf: controllers only, written down.
func TestContainersRemoteOps(t *testing.T) {
	checkArgs(t, OpContainersSearch, []string{`{"term":""}`, `{"term":"-x"}`, `{"term":"a\nb"}`}, []string{`{"term":"postgres"}`})
	checkRow(t, OpContainersSearch, RoleController, false, AuditYes)
	checkArgs(t, OpContainersTags, []string{`{"ref":""}`, `{"ref":"-a"}`}, []string{`{"ref":"library/nginx"}`, `{"ref":"ghcr.io/acme/api"}`})
	checkRow(t, OpContainersTags, RoleController, false, AuditYes)
}

// Spec: pause, tag, create, compose up / restart, the session's context,
// start all services and the services.json template ask in the page
// (Cancel first) and run at once; they never remove anything.
func TestContainersPageOps(t *testing.T) {
	checkArgs(t, OpContainersPause, []string{`{"id":"x"}`}, []string{`{"id":"9f3c2e1a1234"}`})
	checkRow(t, OpContainersPause, RoleController, false, AuditYes)
	checkArgs(t, OpContainersTag, []string{`{"ref":"nginx:1.27","tag":"-f"}`, `{"ref":"","tag":"x:1"}`, `{"ref":"nginx:1.27","tag":"a b"}`}, []string{`{"ref":"nginx:1.27","tag":"nginx:stable-local"}`})
	checkRow(t, OpContainersTag, RoleController, false, AuditYes)
	checkArgs(t, OpContainersCreate, []string{`{"kind":"image","name":"x"}`, `{"kind":"network","name":"-x"}`, `{"kind":"volume","name":"a b"}`}, []string{`{"kind":"network","name":"lab"}`, `{"kind":"volume","name":"cache"}`})
	checkRow(t, OpContainersCreate, RoleController, false, AuditYes)
	checkArgs(t, OpContainersCompose, []string{`{"project":"shop","verb":"down"}`, `{"project":"shop","verb":"stop"}`, `{"project":"","verb":"up"}`, `{"project":"-p","verb":"up"}`}, []string{`{"project":"shop","verb":"up"}`, `{"project":"shop","verb":"restart"}`})
	checkRow(t, OpContainersCompose, RoleController, false, AuditYes)
	checkArgs(t, OpContainersContext, []string{`{"name":""}`, `{"name":"-x"}`}, []string{`{"name":"remote"}`})
	checkRow(t, OpContainersContext, RoleController, false, AuditYes)
	checkArgs(t, OpServicesStartAll, nil, []string{`{}`})
	checkRow(t, OpServicesStartAll, RoleController, false, AuditYes)
	checkArgs(t, OpServicesConfigure, nil, []string{`{}`})
	checkRow(t, OpServicesConfigure, RoleController, false, AuditYes)
}

// Spec: what ends work, removes, sends or runs something — a shell or
// attach, pull, push, run and remove of an image, every prune, removing a
// network or volume, compose stop and down, the machine's default
// context, a registry sign-in or sign-out, adding a database and stopping
// every service — waits for a laptop card. A run never carries env
// values; a registry sign-in never carries a password (it is typed on
// the laptop).
func TestContainersLaptopOps(t *testing.T) {
	checkArgs(t, OpContainersExec, []string{`{"id":"9f3c2e1a1234","kind":"rm"}`, `{"id":"x","kind":"shell"}`}, []string{`{"id":"9f3c2e1a1234","kind":"shell"}`, `{"id":"9f3c2e1a1234","kind":"attach"}`})
	checkRow(t, OpContainersExec, RoleController, true, AuditYes)
	checkArgs(t, OpContainersImage,
		[]string{`{"verb":"build","ref":"nginx:1.27"}`, `{"verb":"pull","ref":"-x"}`, `{"verb":"run","ref":"nginx:1.27","name":"-n"}`, `{"verb":"run","ref":"nginx:1.27","ports":["-p"]}`},
		[]string{`{"verb":"pull","ref":"postgres:17"}`, `{"verb":"push","ref":"ghcr.io/acme/api:1.4"}`, `{"verb":"remove","ref":"nginx:1.27"}`, `{"verb":"run","ref":"nginx:1.27","name":"web","ports":["8080:80"]}`})
	checkRow(t, OpContainersImage, RoleController, true, AuditYes)
	checkArgs(t, OpContainersPrune, []string{`{"kind":"everything"}`, `{"kind":""}`}, []string{`{"kind":"containers"}`, `{"kind":"images"}`, `{"kind":"images-all"}`, `{"kind":"networks"}`, `{"kind":"volumes"}`, `{"kind":"system"}`})
	checkRow(t, OpContainersPrune, RoleController, true, AuditYes)
	checkArgs(t, OpContainersStorageRemove, []string{`{"kind":"image","name":"x"}`, `{"kind":"volume","name":"-x"}`}, []string{`{"kind":"network","name":"lab"}`, `{"kind":"volume","name":"cache"}`})
	checkRow(t, OpContainersStorageRemove, RoleController, true, AuditYes)
	checkArgs(t, OpContainersComposeAsk, []string{`{"project":"shop","verb":"up"}`, `{"project":"shop","verb":"rm"}`}, []string{`{"project":"shop","verb":"stop"}`, `{"project":"shop","verb":"down"}`})
	checkRow(t, OpContainersComposeAsk, RoleController, true, AuditYes)
	checkArgs(t, OpContainersContextDefault, []string{`{"name":"-x"}`}, []string{`{"name":"remote"}`})
	checkRow(t, OpContainersContextDefault, RoleController, true, AuditYes)
	checkArgs(t, OpContainersRegistry, []string{`{"verb":"login","host":"ghcr.io"}`, `{"verb":"connect","host":"-x"}`}, []string{`{"verb":"connect","host":"ghcr.io"}`, `{"verb":"connect","host":""}`, `{"verb":"signout","host":"ghcr.io"}`})
	checkRow(t, OpContainersRegistry, RoleController, true, AuditYes)
	checkArgs(t, OpContainersOpenDB, []string{`{"id":"pg"}`}, []string{`{"id":"9f3c2e1a1234"}`})
	checkRow(t, OpContainersOpenDB, RoleController, true, AuditYes)
	checkArgs(t, OpServicesStopAll, nil, []string{`{}`})
	checkRow(t, OpServicesStopAll, RoleController, true, AuditYes)
	// No field carries an env value or a password: such args are refused.
	for _, c := range [][2]string{{OpContainersImage, `{"verb":"run","ref":"nginx:1.27","env":["A=b"]}`}, {OpContainersImage, `{"verb":"pull","ref":"nginx","password":"x"}`},
		{OpContainersRegistry, `{"verb":"connect","host":"ghcr.io","password":"x"}`}} {
		if _, err := DecodeArgs(c[0], json.RawMessage(c[1])); err == nil {
			t.Errorf("%s %s was taken", c[0], c[1])
		}
	}
}
