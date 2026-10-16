//go:build !unix

package services

import "os/exec"

func setProcGroup(*exec.Cmd) {}

func killGroup(int) {}
