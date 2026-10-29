package containers

// StatWindow is how many samples a Series keeps (2 s apart: two minutes).
const StatWindow = 60

// Series keeps the last StatWindow samples of one container, oldest first.
type Series struct {
	Samples []Sample
}

// Push adds a sample and drops the oldest beyond StatWindow.
func (s *Series) Push(v Sample) {
	s.Samples = append(s.Samples, v)
	if n := len(s.Samples); n > StatWindow {
		s.Samples = append(s.Samples[:0], s.Samples[n-StatWindow:]...)
	}
}

// Last is the newest sample (ok false when empty).
func (s *Series) Last() (Sample, bool) {
	if s == nil || len(s.Samples) == 0 {
		return Sample{}, false
	}
	return s.Samples[len(s.Samples)-1], true
}
