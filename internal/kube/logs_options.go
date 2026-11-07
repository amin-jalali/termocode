package kube

import (
	"context"
	"strconv"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
)

// maxLogRequests bounds the streams of one deployment's logs (kubectl's
// default of 5 stops bigger deployments).
const maxLogRequests = 20

// LogOptions picks which log lines kubectl shows. The zero value is the
// plain logs of the running container.
type LogOptions struct {
	Previous   bool          // the container before the last restart
	Since      time.Duration // only lines newer than this (0 = all)
	Timestamps bool          // a time stamp before each line
}

// Args are the kubectl flags of the options.
func (o LogOptions) Args() []string {
	var out []string
	if o.Previous {
		out = append(out, "--previous")
	}
	if o.Since > 0 {
		out = append(out, "--since", o.Since.String())
	}
	if o.Timestamps {
		out = append(out, "--timestamps")
	}
	return out
}

// suffix is what the tab label adds for the options.
func (o LogOptions) suffix() string {
	switch {
	case o.Previous:
		return " (previous)"
	case o.Since > 0:
		return " (last " + o.Since.String() + ")"
	}
	return ""
}

// logsHead is "logs [--follow] --tail N <options>"; the previous
// container has ended, so there is nothing to follow.
func logsHead(o LogOptions, lines int) []string {
	args := []string{"logs"}
	if !o.Previous {
		args = append(args, "--follow")
	}
	args = append(args, "--tail", strconv.Itoa(lines))
	return append(args, o.Args()...)
}

// LogsCommandWith follows one pod's logs with options in a terminal tab
// (container "" = the pod's only or default container).
func (c Client) LogsCommandWith(pod, container string, o LogOptions) services.Command {
	argv := append([]string{c.Binary}, logsHead(o, 200)...)
	argv = append(argv, c.scope(true)...)
	argv = append(argv, pod)
	if container != "" {
		argv = append(argv, "--container", container)
	}
	return services.Command{Label: "logs: " + shown(pod, container) + o.suffix(), Argv: argv}
}

// DeploymentLogsCommand follows the logs of every pod and container of a
// deployment in one tab; each line starts with its pod and container.
func (c Client) DeploymentLogsCommand(name string, o LogOptions) services.Command {
	argv := append([]string{c.Binary}, logsHead(o, 200)...)
	argv = append(argv, "--all-pods", "--all-containers", "--prefix", "--max-log-requests", strconv.Itoa(maxLogRequests))
	argv = append(argv, c.scope(true)...)
	argv = append(argv, "deployment/"+name)
	return services.Command{Label: "logs: deployment/" + name + o.suffix(), Argv: argv}
}

// LogsWith reads one pod's logs once with options (lines < 0 = every
// line, for saving them to a file).
func (c Client) LogsWith(ctx context.Context, pod, container string, lines int, o LogOptions) (string, error) {
	if lines < 0 {
		lines = -1
	}
	args := append([]string{"logs", "--tail", strconv.Itoa(lines)}, o.Args()...)
	args = append(args, c.scope(true)...)
	args = append(args, pod)
	if container != "" {
		args = append(args, "--container", container)
	}
	return c.capture(ctx, args...)
}
