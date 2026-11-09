package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/containers"
)

// registry_manage: the REGISTRIES rows of the Containers view and the
// palette's Containers: … Registry rows.
//
//   - connect opens the GUI's own sign-in prompt (Connect Registry…): the
//     user types the user and the token there; the model never sees them.
//     It runs at once: the prompt is the user's own question.
//   - signout is the GUI's card: it asks every time, no "Always allow".
//   - catalog reads a registry's repositories at once (Browse Registry
//     Catalog…), with the saved login only when the user opted in.
//   - delete_tag is the GUI's card (Delete tag from registry…): the
//     registry deletes the manifest the tag points to, for everyone.
//
// Docker Hub search and a repository's tags are images_manage search /
// tags.

var aiRegistryOps = []string{"connect", "signout", "catalog", "delete_tag"}

// aiRegistryReq is one checked registry_manage call.
type aiRegistryReq struct {
	op   string
	h    aiContainersHost
	host string
	repo containers.RemoteRepo
	tag  string
}

func aiRegistryParse(ctx context.Context, call aiIDECaller, a map[string]any) (aiRegistryReq, error) {
	r := aiRegistryReq{op: ai.ArgString(a, "op"), host: strings.TrimSpace(ai.ArgString(a, "host"))}
	if !contains(aiRegistryOps, r.op) {
		return r, errors.New("give op: " + strings.Join(aiRegistryOps, ", "))
	}
	switch r.op {
	case "connect", "signout", "catalog":
		if r.host == "" && r.op != "connect" {
			return r, errors.New("give host, like ghcr.io (registries_list lists them)")
		}
		if r.host != "" {
			if err := containers.ValidRegistryHost(r.host); err != nil {
				return r, err
			}
		}
	case "delete_tag":
		ref := strings.TrimSpace(ai.ArgString(a, "ref"))
		i := strings.LastIndexByte(ref, ':')
		if i < 0 || i < strings.LastIndexByte(ref, '/') {
			return r, errors.New("ref: give repository:tag, like ghcr.io/acme/api:1.4")
		}
		repo, err := containers.ParseRemoteRepo(ref[:i])
		if err != nil {
			return r, err
		}
		r.repo, r.tag, r.host = repo, ref[i+1:], repo.Host
	}
	if call == nil {
		return r, nil
	}
	h, err := aiContainersGetHost(ctx, call)
	if err != nil {
		return r, err
	}
	r.h = h
	if r.op == "signout" {
		cfg, err := containers.ReadRegistryConfig(registryConfigPath(h.e.Kind))
		if err != nil && !os.IsNotExist(err) {
			return r, fmt.Errorf("cannot read %s", filepath.Base(registryConfigPath(h.e.Kind)))
		}
		signed := false
		for _, reg := range cfg.Registries {
			if containers.RegistryDisplayHost(reg.Host) == containers.RegistryDisplayHost(r.host) && reg.SignedIn {
				signed, r.host = true, reg.Host
			}
		}
		if !signed {
			return r, fmt.Errorf("%s has no sign-in here (registries_list)", r.host)
		}
	}
	return r, nil
}

func aiRegistryDescribe(call aiIDECaller, a map[string]any) string {
	op := ai.ArgString(a, "op")
	ctx, cancel := aiContainersDescribeCtx()
	defer cancel()
	r, err := aiRegistryParse(ctx, call, a)
	if err != nil {
		return aiTitle(strings.ReplaceAll(op, "_", " ")) + " registry (refused: " + aiOneLineClip(err.Error(), 120) + ")"
	}
	switch r.op {
	case "signout":
		return fmt.Sprintf("Sign out of %s (%s logout %s): the engine forgets the saved login for this host. Local images stay; "+
			"private pulls and pushes need a new sign-in.", r.host, r.h.bin(), r.host)
	case "delete_tag":
		return fmt.Sprintf("Delete %s:%s from the registry: the registry deletes the manifest it points to; every tag of that manifest "+
			"goes too, for everyone who pulls from %s. Local images stay. This cannot be undone.", r.repo.Ref(), r.tag, r.repo.Host)
	case "connect":
		return "Connect registry " + r.host + " (the IDE asks the user for the user and token)"
	}
	return "Browse the catalog of " + r.host
}

func aiRegistryRun(ctx context.Context, call aiIDECaller, a map[string]any) (string, error) {
	r, err := aiRegistryParse(ctx, call, a)
	if err != nil {
		return "", err
	}
	switch r.op {
	case "connect":
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			m.webServicesState()
			m.startRegistryConnect(r.host)
			where := r.host
			if where == "" {
				where = "a registry (the IDE lets the user pick it)"
			}
			return "the IDE asks the user for the user name and token of " + where + "; they are typed there and never reach you. " +
				"registries_list shows the sign-in after.", nil, nil
		})
	case "signout":
		_, err := aiEngineRun(ctx, r.h, aiContainersCallWait, "logout", r.host)
		if err != nil {
			return "", fmt.Errorf("could not sign out of %s: %v", r.host, err)
		}
		_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
			if m.services != nil {
				delete(m.services.registryUsers, containers.RegistryDisplayHost(r.host))
			}
			return "", m.containersRefresh("registries"), nil
		})
		return "signed out of " + r.host + " (`" + r.h.bin() + " logout " + r.host + "`)", nil
	}
	mk, err := aiRegistryAPI(ctx, call, r.host)
	if err != nil {
		return "", err
	}
	api, err := mk()
	if err != nil {
		return "", err
	}
	cctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if r.op == "catalog" {
		cat, err := api.Catalog(cctx, r.host)
		if err != nil {
			return "", fmt.Errorf("cannot read the catalog of %s: %s", r.host, errLine(ai.Redact(err.Error())))
		}
		more := ""
		if cat.Truncated {
			more = " (more on the registry)"
		}
		return fmt.Sprintf("Repositories of %s: %d%s (images_manage op tags lists a repository's tags)\n%s", r.host, len(cat.Repos), more, strings.Join(cat.Repos, "\n")), nil
	}
	d, err := api.ManifestDigest(cctx, r.repo, r.tag)
	if err == nil {
		err = api.DeleteManifest(cctx, r.repo, d)
	}
	if err != nil {
		return "", fmt.Errorf("cannot delete %s:%s: %s (registry_manage op connect signs in first)", r.repo.Ref(), r.tag, errLine(ai.Redact(err.Error())))
	}
	return "deleted " + r.repo.Ref() + ":" + r.tag + " (manifest " + d + ") from the registry", nil
}

// aiRegistryManageTool is registry_manage.
func aiRegistryManageTool(call aiIDECaller) ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	return ai.Tool{
		Name: "registry_manage",
		Description: "Container registries, as the Containers view's REGISTRIES. op connect (host, optional) opens the IDE's sign-in " +
			"prompt: the user types the user and token there and you never see them; signout (host) asks the user every time; " +
			"catalog (host) lists a registry's repositories; delete_tag (ref: repository:tag) deletes it from the registry for " +
			"everyone and asks the user every time. images_manage search / tags reads Docker Hub and a repository's tags.",
		Params: map[string]any{
			"op":   map[string]any{"type": "string", "enum": aiRegistryOps, "description": "what to do"},
			"host": str("the registry host, like ghcr.io or registry.example.com:5000"),
			"ref":  str("delete_tag: repository:tag"),
		},
		Required: []string{"op"},
		Mutating: true,
		Gate: func(a map[string]any) (bool, string) {
			switch ai.ArgString(a, "op") {
			case "connect", "catalog":
				return false, ""
			}
			return true, ""
		},
		Validate: func(a map[string]any) error {
			ctx, cancel := context.WithTimeout(context.Background(), aiContainersCallWait)
			defer cancel()
			_, err := aiRegistryParse(ctx, call, a)
			return err
		},
		Describe: func(a map[string]any) string { return aiRegistryDescribe(call, a) },
		Run:      func(ctx context.Context, a map[string]any) (string, error) { return aiRegistryRun(ctx, call, a) },
	}
}
