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

func TestCombinedRetainedDurationIgnoresStrayMidnightBadge(t *testing.T) {
	// Regression: a badge shortly after midnight (tail end of the previous
	// evening's departure) must not anchor FirstAccess and get clamped up to
	// 08:00, or a single legitimate evening badge would inflate the reported
	// presence to a flat, false 12h with nothing actually recorded in between.
	events := []BadgeEvent{
		{Timestamp: mustParisTime(t, "2026-04-20 00:40:00")},
		{Timestamp: mustParisTime(t, "2026-04-20 20:21:35")},
	}

	duration := CombinedRetainedDuration(events, time.Time{}, time.Time{}, nil)
	if duration != 0 {
		t.Fatalf("expected 0 duration when the only badges are a stray midnight one and a single evening one, got %s", duration)
	}
}

func TestCombinedRetainedDurationKeepsBadgesInsideAttendanceWindow(t *testing.T) {
	events := []BadgeEvent{
		{Timestamp: mustParisTime(t, "2026-04-20 00:40:00")},
		{Timestamp: mustParisTime(t, "2026-04-20 09:00:00")},
		{Timestamp: mustParisTime(t, "2026-04-20 17:00:00")},
	}

	duration := CombinedRetainedDuration(events, time.Time{}, time.Time{}, nil)
	expected := 8 * time.Hour
	if duration != expected {
		t.Fatalf("expected the stray midnight badge to be ignored and the window kept as [09:00,17:00] (%s), got %s", expected, duration)
	}
}
