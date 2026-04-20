package watchdog

import (
	"testing"
	"time"
)

func mustParisTime(t *testing.T, value string) time.Time {
	t.Helper()
	loc := parisLocation()
	ts, err := time.ParseInLocation("2006-01-02 15:04:05", value, loc)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return ts
}

func TestRetainedAccessWindowRejectsCrossDayRanges(t *testing.T) {
	start := mustParisTime(t, "2026-04-19 08:00:00")
	end := mustParisTime(t, "2026-04-20 19:02:44")

	_, _, ok := retainedAccessWindow(start, end)
	if ok {
		t.Fatalf("expected cross-day retained window to be rejected")
	}
}

func TestCombinedRetainedDurationSingleBadgeDoesNotReuseAnotherDay(t *testing.T) {
	events := []BadgeEvent{
		{Timestamp: mustParisTime(t, "2026-04-20 19:02:44")},
	}
	fallbackFirst := mustParisTime(t, "2026-04-19 08:00:00")
	fallbackLast := mustParisTime(t, "2026-04-20 19:02:44")

	duration := CombinedRetainedDuration(events, fallbackFirst, fallbackLast, nil)
	if duration != 0 {
		t.Fatalf("expected 0 duration for a single badge event, got %s", duration)
	}
}

func TestCombinedRetainedDurationIgnoresSessionsFromAnotherDay(t *testing.T) {
	events := []BadgeEvent{
		{Timestamp: mustParisTime(t, "2026-04-20 08:00:00")},
		{Timestamp: mustParisTime(t, "2026-04-20 12:00:00")},
	}
	sessions := []LocationSession{
		{
			BeginAt: mustParisTime(t, "2026-04-19 09:00:00"),
			EndAt:   mustParisTime(t, "2026-04-19 11:00:00"),
		},
		{
			BeginAt: mustParisTime(t, "2026-04-20 13:00:00"),
			EndAt:   mustParisTime(t, "2026-04-20 15:00:00"),
		},
	}

	duration := CombinedRetainedDuration(events, time.Time{}, time.Time{}, sessions)
	expected := 6 * time.Hour
	if duration != expected {
		t.Fatalf("expected %s, got %s", expected, duration)
	}
}
