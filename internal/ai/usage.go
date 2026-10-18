package ai

// Usage is the token count of one or more model requests, as the
// provider reported it. Input does not include the cached tokens: a
// request's whole prompt is Input + CacheRead + CacheWrite.
type Usage struct {
	// Model is the id the provider answered with (it may carry a date
	// suffix, e.g. gpt-4.1-mini-2025-04-14); "" when it did not say.
	Model      string `json:"model,omitempty"`
	Input      int    `json:"input,omitempty"`       // prompt tokens read at the full price
	Output     int    `json:"output,omitempty"`      // generated tokens (thinking included)
	CacheRead  int    `json:"cache_read,omitempty"`  // prompt tokens read from the cache
	CacheWrite int    `json:"cache_write,omitempty"` // prompt tokens written to the cache
	// Requests is how many requests the counts cover.
	Requests int `json:"requests,omitempty"`
	// PeakContext is the biggest prompt of one request
	// (Input + CacheRead + CacheWrite): the context window use.
	PeakContext int `json:"peak_context,omitempty"`
}

// Add returns u + v. A nil side counts as nothing; nil + nil is nil. The
// model is v's when v names one. Neither side is changed.
func (u *Usage) Add(v *Usage) *Usage {
	if v == nil {
		return u
	}
	if u == nil {
		c := *v
		return &c
	}
	s := *u
	if v.Model != "" {
		s.Model = v.Model
	}
	s.Input += v.Input
	s.Output += v.Output
	s.CacheRead += v.CacheRead
	s.CacheWrite += v.CacheWrite
	s.Requests += v.Requests
	s.PeakContext = max(s.PeakContext, v.PeakContext)
	return &s
}

// finishUsage completes the counts of one request: Requests is 1 and
// PeakContext the prompt size. A request with no counts at all is nil.
func finishUsage(model string, in, out, cacheRead, cacheWrite int, seen bool) *Usage {
	if !seen {
		return nil
	}
	return &Usage{Model: model, Input: in, Output: out, CacheRead: cacheRead, CacheWrite: cacheWrite,
		Requests: 1, PeakContext: in + cacheRead + cacheWrite}
}
