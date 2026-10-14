package git

import (
	"strconv"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

func TestRelativeDateMatchesGit(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	t.Cleanup(clock.Set(func() time.Time { return now }))
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "30 seconds ago"},
		{5 * time.Minute, "5 minutes ago"},
		{1 * time.Hour, "60 minutes ago"},
		{2 * time.Hour, "2 hours ago"},
		{24 * time.Hour, "24 hours ago"},
		{3 * 24 * time.Hour, "3 days ago"},
		{1 * 24 * time.Hour * 15, "2 weeks ago"},
		{100 * 24 * time.Hour, "3 months ago"},
		{365 * 24 * time.Hour, "1 year ago"},
		{430 * 24 * time.Hour, "1 year, 2 months ago"},
		{3000 * 24 * time.Hour, "8 years ago"},
		{-time.Hour, "in the future"},
	}
	for _, c := range cases {
		epoch := strconv.FormatInt(now.Add(-c.ago).Unix(), 10)
		if got := relativeDate(epoch); got != c.want {
			t.Errorf("relativeDate(-%v) = %q, want %q", c.ago, got, c.want)
		}
	}
	if got := relativeDate("x"); got != "" {
		t.Errorf("relativeDate(bad) = %q", got)
	}
}
