// Package devcontainer reads a project's devcontainer.json (JSONC) and
// lists the lifecycle commands it would run, so a trust card can show
// every command and where it runs before termocode creates a container.
// It never runs anything itself.
package devcontainer
