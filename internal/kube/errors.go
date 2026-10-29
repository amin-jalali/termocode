package kube

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// ErrKind sorts a failed kubectl call.
type ErrKind int

const (
	ErrOther     ErrKind = iota
	ErrNoKubectl         // kubectl is not installed
	ErrNoConfig          // no kubeconfig or no current context
	ErrNoCluster         // the API server does not answer
	ErrForbidden         // RBAC says no
	ErrTimeout           // the call ran out of time
)

// Error is a failed call as one short line for the view.
type Error struct {
	Kind ErrKind
	Line string // kubectl's own first line (ErrOther, ErrForbidden)
}

// maxLine caps a kubectl line in the view.
const maxLine = 120

func (e *Error) Error() string {
	switch e.Kind {
	case ErrNoKubectl:
		return "kubectl is not installed"
	case ErrNoConfig:
		return "No cluster set up: kubeconfig has no current context"
	case ErrNoCluster:
		return "Cannot reach the cluster"
	case ErrTimeout:
		return "kubectl took too long; the cluster may be down"
	case ErrForbidden:
		if e.Line != "" {
			return "Not allowed: " + e.Line
		}
		return "Not allowed by the cluster"
	}
	if e.Line != "" {
		return e.Line
	}
	return "kubectl failed"
}

// ErrorLine turns a failed call (its stderr and the start/cancel error)
// into one quiet line.
func ErrorLine(stderr string, err error) error {
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return &Error{Kind: ErrTimeout}
		case errors.Is(err, exec.ErrNotFound) || strings.Contains(strings.ToLower(err.Error()), "no such file"):
			return &Error{Kind: ErrNoKubectl}
		case errors.Is(err, context.Canceled):
			return err
		}
	}
	line := firstLine(stderr)
	low := strings.ToLower(stderr)
	switch {
	case strings.Contains(low, "current-context is not set"),
		strings.Contains(low, "no configuration has been provided"),
		strings.Contains(low, "localhost:8080 was refused"),
		strings.Contains(low, "context was not found"),
		strings.Contains(low, "does not exist") && strings.Contains(low, "context"):
		return &Error{Kind: ErrNoConfig}
	case strings.Contains(low, "forbidden"):
		// `Error from server (Forbidden): pods is forbidden: User "x"
		// cannot list resource "pods" …`: keep what follows "forbidden: ".
		if i := strings.Index(strings.ToLower(line), "forbidden: "); i >= 0 {
			line = line[i+len("forbidden: "):]
		}
		return &Error{Kind: ErrForbidden, Line: clip(line)}
	case strings.Contains(low, "unable to connect to the server"),
		strings.Contains(low, "connection refused"),
		strings.Contains(low, "no such host"),
		strings.Contains(low, "i/o timeout"),
		strings.Contains(low, "network is unreachable"),
		strings.Contains(low, "tls handshake timeout"):
		return &Error{Kind: ErrNoCluster}
	}
	if line == "" && err != nil {
		line = err.Error()
	}
	line = strings.TrimPrefix(line, "error: ")
	line = strings.TrimPrefix(line, "Error: ")
	return &Error{Kind: ErrOther, Line: clip(line)}
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

func clip(s string) string {
	r := []rune(s)
	if len(r) > maxLine {
		return string(r[:maxLine-1]) + "…"
	}
	return s
}

// KindOf is the ErrKind of err (ErrOther for any other error).
func KindOf(err error) ErrKind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return ErrOther
}
