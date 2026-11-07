package setup

import (
	"errors"
	"strings"
	"testing"
)

func TestParseSSHContexts(t *testing.T) {
	out := "default *\tunix:///var/run/docker.sock\nprod-ssh\tssh://me@prod:2222\nbad\nremote\ttcp://h:2376\n"
	got := parseSSHContexts(out)
	if len(got) != 1 || got[0].Name != "prod-ssh" || got[0].Endpoint != "ssh://me@prod:2222" {
		t.Fatalf("contexts = %+v", got)
	}
}

func TestSSHTarget(t *testing.T) {
	args, ok := sshTarget("ssh://me@prod:2222")
	if !ok || strings.Join(args, " ") != "-p 2222 -- me@prod" {
		t.Fatalf("target = %q %v", args, ok)
	}
	args, ok = sshTarget("ssh://box")
	if !ok || strings.Join(args, " ") != "-- box" {
		t.Fatalf("target = %q %v", args, ok)
	}
	for _, bad := range []string{"tcp://h", "ssh://-oProxyCommand=x", "ssh://-x@h", "::"} {
		if _, ok := sshTarget(bad); ok {
			t.Errorf("sshTarget(%q) accepted", bad)
		}
	}
}

func TestControlMasterOn(t *testing.T) {
	if !controlMasterOn("user me\ncontrolmaster auto\ncontrolpath /home/me/.ssh/cm-%r@%h:%p\n") {
		t.Fatal("auto + path should be on")
	}
	for _, g := range []string{"controlmaster false\ncontrolpath /x\n", "controlmaster auto\ncontrolpath none\n", "controlmaster auto\n"} {
		if controlMasterOn(g) {
			t.Errorf("on for %q", g)
		}
	}
}

func TestContainerSSHChecks(t *testing.T) {
	oldLook, oldRun := lookPath, runOutput
	defer func() { lookPath, runOutput = oldLook, oldRun }()
	lookPath = func(bin string) (string, error) { return "/usr/bin/" + bin, nil }
	var sshArgs [][]string
	runOutput = func(name string, args ...string) (string, error) {
		switch name {
		case "docker":
			return "fast\tssh://me@fast\nslow\tssh://slow:22\n", nil
		case "ssh":
			sshArgs = append(sshArgs, args)
			if args[len(args)-1] == "me@fast" {
				return "controlmaster auto\ncontrolpath ~/.ssh/cm\n", nil
			}
			return "controlmaster false\n", nil
		}
		return "", errors.New("unexpected " + name)
	}
	checks := ContainerSSHChecks()
	if len(checks) != 2 || checks[0].Status != OK || checks[1].Status != Warn {
		t.Fatalf("checks = %+v", checks)
	}
	if !strings.Contains(checks[1].Detail, "Host slow") || !strings.Contains(checks[1].Detail, "ControlMaster auto") {
		t.Fatalf("hint = %q", checks[1].Detail)
	}
	if len(sshArgs) != 2 || sshArgs[0][0] != "-G" {
		t.Fatalf("ssh args = %q", sshArgs)
	}
	lookPath = func(string) (string, error) { return "", errors.New("no") }
	if got := ContainerSSHChecks(); got != nil {
		t.Fatalf("without docker: %+v", got)
	}
}
