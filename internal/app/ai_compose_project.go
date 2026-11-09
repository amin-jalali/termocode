package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
)

// compose_project: a compose project of any host as the Containers
// view's project row, its menu and the palette's Containers: … Project
// rows do it: up, stop, restart, recreate (up --force-recreate), down,
// down with volumes, pull, scale and logs; and Deploy Stack from Git.
// The project runs in its own folder (it must be on this machine) in a
// terminal tab, as the GUI's.
//
// It asks where the GUI asks: stop, down and down with volumes are the
// GUI's cards (the page's laptop cards) and ask every time with no
// "Always allow"; up, restart, recreate, pull and scale ask with the
// containers class (the page's in-page confirm); logs read at once;
// deploy_git opens the GUI's own Deploy card, which the user answers.

var aiComposeOps = []string{"up", "stop", "restart", "recreate", "down", "down_volumes", "pull", "scale", "logs", "deploy_git"}

var aiComposeCards = map[string]bool{"stop": true, "down": true, "down_volumes": true}

// aiComposeVerb is the compose argv of op.
func aiComposeVerb(op string, a map[string]any) ([]string, error) {
	switch op {
	case "up":
		return []string{"up", "-d"}, nil
	case "recreate":
		return []string{"up", "-d", "--force-recreate"}, nil
	case "down_volumes":
		return []string{"down", "--volumes"}, nil
	case "scale":
		svc := strings.TrimSpace(ai.ArgString(a, "service"))
		if err := containers.ValidName(svc); err != nil {
			return nil, fmt.Errorf("service: %v", err)
		}
		n := ai.ArgInt(a, "replicas")
		if n < 0 || n > svcMaxScale {
			return nil, fmt.Errorf("replicas must be a whole number from 0 to %d", svcMaxScale)
		}
		return []string{"up", "-d", "--scale", svc + "=" + strconv.Itoa(n)}, nil
	case "logs":
		lines := ai.ArgInt(a, "lines")
		if lines <= 0 {
			lines = 100
		}
		return []string{"logs", "--no-color", "--timestamps", "--tail", strconv.Itoa(min(lines, aiContainersMaxLines))}, nil
	}
	return []string{op}, nil
}

var aiComposeCardText = map[string][2]string{
	"stop":         {"Stop project", "What runs in its containers ends."},
	"down":         {"Remove project containers", "This stops and removes its containers and networks. Volumes stay."},
	"down_volumes": {"Remove project containers and volumes", "This stops and removes its containers and networks and deletes its volumes: the data in the volumes is lost. This cannot be undone."},
}

func aiComposeCommand(t aiComposeTarget, op string, a map[string]any) (services.Command, error) {
	verb, err := aiComposeVerb(op, a)
	if err != nil {
		return services.Command{}, err
	}
	args := append(verb, t.services...)
	cmd := t.h.termCommand(t.h.e.ComposeWithFiles(t.project, t.dir, t.files, args...))
	cmd.Label = "compose " + strings.ReplaceAll(op, "_", " ") + ": " + t.project
	return cmd, nil
}

func aiComposeProjectDescribe(call aiIDECaller, a map[string]any) string {
	op, project := ai.ArgString(a, "op"), strings.TrimSpace(ai.ArgString(a, "project"))
	if op == "deploy_git" {
		return "Deploy a stack from Git (" + aiOneLineClip(ai.ArgString(a, "url"), 80) + "): the IDE's Deploy card asks the user"
	}
	ctx, cancel := aiContainersDescribeCtx()
	defer cancel()
	t, err := aiComposeResolve(ctx, call, a)
	if err != nil {
		return aiTitle(op) + " compose project " + aiOneLineClip(project, 60) + " (refused: " + aiOneLineClip(err.Error(), 120) + ")"
	}
	cmd, err := aiComposeCommand(t, op, a)
	if err != nil {
		return aiTitle(op) + " compose project " + project + " (refused: " + err.Error() + ")"
	}
	argv := append([]string{t.h.bin()}, cmd.Argv[1:]...)
	if txt, ok := aiComposeCardText[op]; ok {
		return fmt.Sprintf("%s %s (%s) in a terminal tab · %s · %s", txt[0], t.project, svcShellLine(argv), t.h.contextLabel(ctx), txt[1])
	}
	return fmt.Sprintf("%s compose project %s (%s) · %s", aiTitle(strings.ReplaceAll(op, "_", " ")), t.project, svcShellLine(argv), t.h.contextLabel(ctx))
}

func aiComposeProjectRun(ctx context.Context, call aiIDECaller, a map[string]any) (string, error) {
	op := ai.ArgString(a, "op")
	if op == "deploy_git" {
		url := strings.TrimSpace(ai.ArgString(a, "url"))
		if _, err := services.PlanGitDeploy(url, services.GitDeployBase()); err != nil {
			return "", err
		}
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			m.webServicesState()
			return "the IDE shows its Deploy card for " + url + " (the exact clone and compose commands); the user answers it. " +
				"Nothing runs before that; containers_list shows the project after.", m.onDeployGitPrompt(url), nil
		})
	}
	t, err := aiComposeResolve(ctx, call, a)
	if err != nil {
		return "", err
	}
	cmd, err := aiComposeCommand(t, op, a)
	if err != nil {
		return "", err
	}
	if op == "logs" {
		out, err := aiEngineRun(ctx, t.h, aiContainersCallWait, cmd.Argv[1:]...)
		if err != nil {
			return "", fmt.Errorf("compose logs failed: %v", err)
		}
		out = strings.TrimRight(out, "\n")
		out, n := aiServicesRedact(out)
		head := fmt.Sprintf("%s\nLogs of compose project %s: [%d redacted values]", t.h.header(ctx), t.project, n)
		if out == "" {
			return head + "\n(no log output)", nil
		}
		return head + "\n" + aiCap(out, loadServicesSettings().MaxLogKB, true), nil
	}
	key := containerTabPrefix + "compose:" + t.h.tabKey(t.project)
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		if err := aiCtrTerminal(m, cmd, key); err != nil {
			return "", nil, fmt.Errorf("cannot open the compose tab: %v", err)
		}
		return "", nil, nil
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("started `%s` in the terminal tab %q · %s; containers_list shows the state",
		svcShellLine(append([]string{t.h.bin()}, cmd.Argv[1:]...)), cmd.Label, t.h.contextLabel(ctx)), nil
}

// aiComposeProjectTool is compose_project.
func (m *Model) aiComposeProjectTool(call aiIDECaller) ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	session := aiContainersGate(m.services)
	return ai.Tool{
		Name: "compose_project",
		Description: "A compose project of a host (containers_list names them), as the Containers view's project row: up, stop, " +
			"restart, recreate (up --force-recreate), down (containers and networks go, volumes stay), down_volumes (volumes too: " +
			"their data is lost), pull, scale (service, replicas), logs (lines) and deploy_git (url: clone a repository and start its " +
			"compose project; the IDE's own Deploy card asks the user). It runs in the project's folder in a terminal tab. services " +
			"limits it to some services. stop, down and down_volumes ask the user every time; up, restart, recreate, pull and scale " +
			"ask; logs run at once.",
		Params: map[string]any{
			"op":       map[string]any{"type": "string", "enum": aiComposeOps, "description": "what to do"},
			"project":  str("the compose project (containers_list)"),
			"context":  aiContextParam(),
			"services": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "only these services (default all)"},
			"service":  str("scale: the service"), "replicas": map[string]any{"type": "integer", "description": "scale: how many containers (0–100)"},
			"lines": map[string]any{"type": "integer", "description": "logs: how many last lines per service (default 100, max 1000)"},
			"url":   str("deploy_git: an https://, ssh:// or user@host:path repository URL (#branch or #tag picks one)"),
		},
		Required: []string{"op"},
		Mutating: true,
		Gate: func(a map[string]any) (bool, string) {
			op := ai.ArgString(a, "op")
			switch {
			case op == "logs" || op == "deploy_git":
				return false, ""
			case aiComposeCards[op] || !contains(aiComposeOps, op):
				return true, ""
			}
			return session(a)
		},
		Validate: func(a map[string]any) error {
			op := ai.ArgString(a, "op")
			if !contains(aiComposeOps, op) {
				return errors.New("give op: " + strings.Join(aiComposeOps, ", "))
			}
			if op == "deploy_git" {
				_, err := services.PlanGitDeploy(strings.TrimSpace(ai.ArgString(a, "url")), services.GitDeployBase())
				return err
			}
			if _, err := aiComposeVerb(op, a); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), aiContainersCallWait)
			defer cancel()
			_, err := aiComposeResolve(ctx, call, a)
			return err
		},
		Describe: func(a map[string]any) string { return aiComposeProjectDescribe(call, a) },
		Run:      func(ctx context.Context, a map[string]any) (string, error) { return aiComposeProjectRun(ctx, call, a) },
	}
}
