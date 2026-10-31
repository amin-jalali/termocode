package ai

import (
	"context"
	"sync"
)

// Tool images (MCPX-11): a tool's Run may hand pictures (a screenshot,
// a chart from an MCP server) to the model with AttachImages. The agent
// collects them on the call's RoleTool message, but only when the model
// takes images (native tool calling and SupportsImages); otherwise
// AttachImages says false and the tool writes a short text note.

type imageSinkKey struct{}

type imageSink struct {
	mu     sync.Mutex
	images []Image
}

// AttachImages sends imgs to the model with the running tool's result.
// It reports false when the model cannot see images (no call, the text
// protocol or a text-only model); then nothing is sent.
func AttachImages(ctx context.Context, imgs ...Image) bool {
	s, _ := ctx.Value(imageSinkKey{}).(*imageSink)
	if s == nil {
		return false
	}
	s.mu.Lock()
	s.images = append(s.images, imgs...)
	s.mu.Unlock()
	return true
}

// withImageSink returns ctx with a sink when the agent's model takes
// images, and the function that takes the collected images.
func (a *Agent) withImageSink(ctx context.Context) (context.Context, func() []Image) {
	if !a.Native || !SupportsImages(a.Provider) {
		return ctx, func() []Image { return nil }
	}
	s := &imageSink{}
	return context.WithValue(ctx, imageSinkKey{}, s), func() []Image {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := s.images
		s.images = nil
		return out
	}
}
