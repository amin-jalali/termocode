//go:build unix

package webgw

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// FuzzLaunchSecretRedeem: "$S" in a guess stands for the real secret, so
// the fuzzer can try near misses around it. Invariants: only the exact
// secret redeems; it redeems once; a wrong guess never panics and never
// opens the secret; a single wrong guess does not lock the owner out.
func FuzzLaunchSecretRedeem(f *testing.F) {
	for _, seed := range []string{"$S", "", "x", "$S$S", " $S", "$S ", "$S\x00", "$S\n",
		"A$S", strings.Repeat("A", 43), "\xff\xfe", "%24S"} {
		f.Add(seed, int64(0))
	}
	f.Add("$S", int64(LaunchSecretTTL))
	f.Add("$S", int64(-1))
	f.Fuzz(func(t *testing.T, tmpl string, after int64) {
		plain, s, err := NewLaunchSecret(t0)
		if err != nil {
			t.Fatal(err)
		}
		guess := strings.ReplaceAll(tmpl, "$S", plain)
		now := t0.Add(time.Duration(after))
		inTime := now.Before(t0.Add(LaunchSecretTTL))
		err = s.Redeem(guess, now)
		switch {
		case guess == plain && inTime:
			if err != nil {
				t.Fatalf("exact secret in time refused: %v", err)
			}
			if err := s.Redeem(plain, now); !errors.Is(err, ErrSecretUsed) {
				t.Fatalf("second redeem: %v", err)
			}
		case !inTime:
			if !errors.Is(err, ErrSecretExpired) {
				t.Fatalf("late redeem: %v", err)
			}
		default:
			if !errors.Is(err, ErrSecretWrong) {
				t.Fatalf("wrong guess %q: %v", guess, err)
			}
			if err := s.Redeem(plain, now); err != nil {
				t.Fatalf("right secret after one wrong guess: %v", err)
			}
		}
	})
}
