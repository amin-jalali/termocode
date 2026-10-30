package ai

// SecretTailHold returns how many bytes at the end of s could be the
// start of a registered secret: the longest suffix of s that is a proper
// prefix of one. A stream that sends text as it arrives (the web chat)
// holds those bytes back until more text shows whether a secret follows,
// so a secret never leaves in two pieces that redaction cannot see.
func SecretTailHold(s string) int {
	if s == "" {
		return 0
	}
	secretsMu.RLock()
	defer secretsMu.RUnlock()
	best := 0
	for k := range secrets {
		for l := min(len(k)-1, len(s)); l > best; l-- {
			if s[len(s)-l:] == k[:l] {
				best = l
				break
			}
		}
	}
	return best
}
