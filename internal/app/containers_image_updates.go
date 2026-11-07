package app

import (
	"context"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/toast"
)

// CTX-36: "Containers: Check Running Images for Updates". For each tag a
// running container uses, the registry's digest of that tag (HEAD of the
// manifest, the saved token only where the user opted in) is compared
// with the local repo digests. A notice only: nothing is pulled or
// restarted. Runs when the user asks, never in the background.

// imageUpdateMax caps the images one check asks about.
const imageUpdateMax = 20

type imageUpdatesMsg struct {
	updates map[string]bool // ref -> a newer image is on the registry
	skipped int             // no local digest, not on a registry, or no answer
}

// imageUpdateTargets are the image tags of the running containers, sorted,
// without digest-pinned ones.
func imageUpdateTargets(st *servicesState) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range st.containerRows {
		ref := strings.TrimSpace(c.Image)
		if c.State != "running" || ref == "" || strings.Contains(ref, "@") || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	sort.Strings(out)
	if len(out) > imageUpdateMax {
		out = out[:imageUpdateMax]
	}
	return out
}

func (m *Model) checkImageUpdates() tea.Cmd {
	st := m.services
	if !st.containerDetected || !st.containerEngine.Reachable {
		return m.servicesToast(toast.Info, "No running engine", "start the engine, then check again")
	}
	refs := imageUpdateTargets(st)
	if len(refs) == 0 {
		return m.servicesToast(toast.Info, "No running containers with a tagged image", "")
	}
	type job struct {
		ref  string
		repo containers.RemoteRepo
		tag  string
		mk   func() (*containers.RegistryAPI, error)
	}
	var jobs []job
	skipped := 0
	for _, ref := range refs {
		repo, err := containers.ParseRemoteRepo(ref)
		if err != nil {
			skipped++
			continue
		}
		tag := "latest"
		if i := strings.LastIndexByte(ref, ':'); i > strings.LastIndexByte(ref, '/') {
			tag = ref[i+1:]
		}
		jobs = append(jobs, job{ref: ref, repo: repo, tag: tag, mk: m.registryAPIFor(repo.Host)})
	}
	e := st.containerEngine
	start := m.servicesToast(toast.Info, "Checking "+itoa(len(jobs))+" images…", "asks each registry for the tag's digest")
	return tea.Batch(start, func() tea.Msg {
		out := imageUpdatesMsg{updates: map[string]bool{}, skipped: skipped}
		for _, j := range jobs {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			b, err := e.InspectRaw(ctx, j.ref)
			var remote string
			if err == nil {
				var api *containers.RegistryAPI
				if api, err = j.mk(); err == nil {
					remote, err = api.ManifestDigest(ctx, j.repo, j.tag)
				}
			}
			cancel()
			if err != nil {
				out.skipped++
				continue
			}
			upd, known := containers.ImageHasUpdate(containers.ImageRepoDigests(b), j.ref, remote)
			if !known {
				out.skipped++
				continue
			}
			out.updates[j.ref] = upd
		}
		return out
	})
}

func (m *Model) onImageUpdates(msg imageUpdatesMsg) tea.Cmd {
	st := m.services
	st.imageUpdates = msg.updates
	st.touch()
	var newer []string
	for ref, upd := range msg.updates {
		if upd {
			newer = append(newer, sanitizeOutputLine(ref))
		}
	}
	sort.Strings(newer)
	note := ""
	if msg.skipped > 0 {
		note = itoa(msg.skipped) + " not checked (local only, private or no answer)"
	}
	if len(newer) == 0 {
		return m.servicesToast(toast.Info, "Running images are up to date", note)
	}
	detail := "u on the image pulls it; Recreate in the container menu restarts with it"
	if note != "" {
		detail = note + " · " + detail
	}
	return m.servicesToast(toast.Info, "Update available: "+strings.Join(newer, ", "), detail)
}
