// Package specui holds behaviour tests for the UI around the editor:
// search, command palette, terminal and bottom panels, settings, themes,
// AI chat, notifications and layout. Each test drives the real app with
// internal/testkit and states its Given / When / Then in a "Spec:" comment.
//
// Tests that start nvim skip with -short. Run them with
//
//	go test ./internal/testkit/specui
package specui
