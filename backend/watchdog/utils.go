package watchdog

import (
	"fmt"
	"sort"
	"time"
)

func formatDuration(d time.Duration) string {
	// Round to nearest second for cleaner output, optional
	d = d.Round(time.Second)

	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 && h < 10 {
		return fmt.Sprintf(" (%dh%02dm%02ds) ", h, m, s)
	} else if h >= 10 {
		return fmt.Sprintf("(%02dh%02dm%02ds) ", h, m, s)
	} else if m > 0 {
		return fmt.Sprintf("  (%dm%02ds)   ", m, s)
	} else if s < 10 {
		return fmt.Sprintf("    (%ds)    ", s)
	} else {
		return fmt.Sprintf("    (%02ds)    ", s)
	}
}

func retainedAccessWindow(first, last time.Time) (time.Time, time.Time, bool) {
	if first.IsZero() || last.IsZero() {
		return time.Time{}, time.Time{}, false
	}

	loc := parisLocation()
	firstInLoc := first.In(loc)
	lastInLoc := last.In(loc)
	if firstInLoc.Year() != lastInLoc.Year() || firstInLoc.Month() != lastInLoc.Month() || firstInLoc.Day() != lastInLoc.Day() {
		return time.Time{}, time.Time{}, false
	}

	dayStart := time.Date(firstInLoc.Year(), firstInLoc.Month(), firstInLoc.Day(), 8, 0, 0, 0, loc)
	dayEnd := time.Date(firstInLoc.Year(), firstInLoc.Month(), firstInLoc.Day(), 20, 0, 0, 0, loc)

	start := firstInLoc
	if start.Before(dayStart) {
		start = dayStart
	}

	end := lastInLoc
	if end.After(dayEnd) {
		end = dayEnd
	}

	if !end.After(start) {
		return start, end, false
	}

	return start, end, true
}

func RetainedDuration(first, last time.Time) time.Duration {
	start, end, ok := retainedAccessWindow(first, last)
	if !ok {
		return 0
	}
	return end.Sub(start)
}

type TimeRange struct {
	Start time.Time
	End   time.Time
}

func timeRangesOverlap(startA, endA, startB, endB time.Time) bool {
	return startA.Before(endB) && endA.After(startB)
}

func isSameParisDay(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	loc := parisLocation()
	aInLoc := a.In(loc)
	bInLoc := b.In(loc)
	return aInLoc.Year() == bInLoc.Year() && aInLoc.Month() == bInLoc.Month() && aInLoc.Day() == bInLoc.Day()
}

func retainedTargetDay(badgeEvents []BadgeEvent, fallbackFirst, fallbackLast time.Time, locationSessions []LocationSession) (time.Time, bool) {
	if len(badgeEvents) > 0 {
		return badgeEvents[0].Timestamp, true
	}
	if !fallbackFirst.IsZero() {
		return fallbackFirst, true
	}
	if !fallbackLast.IsZero() {
		return fallbackLast, true
	}
	for _, session := range locationSessions {
		if !session.BeginAt.IsZero() {
			return session.BeginAt, true
		}
	}
	return time.Time{}, false
}

func badgeRetainedRanges(events []BadgeEvent, fallbackFirst, fallbackLast time.Time) []TimeRange {
	targetDay, ok := retainedTargetDay(events, fallbackFirst, fallbackLast, nil)
	if !ok {
		return nil
	}

	first := fallbackFirst
	last := fallbackLast
	if len(events) > 0 {
		filtered := make([]BadgeEvent, 0, len(events))
		for _, event := range events {
			if isSameParisDay(event.Timestamp, targetDay) {
				filtered = append(filtered, event)
			}
		}
		if len(filtered) == 0 {
			return nil
		}
		first = filtered[0].Timestamp
		last = filtered[len(filtered)-1].Timestamp
	} else {
		if !isSameParisDay(first, targetDay) || !isSameParisDay(last, targetDay) {
			return nil
		}
	}

	start, end, ok := retainedAccessWindow(first, last)
	if !ok {
		return nil
	}
	return []TimeRange{{Start: start, End: end}}
}

func IsCountedLocationSession(session LocationSession) bool {
	start, end := session.BeginAt, session.EndAt
	if session.Ongoing || start.IsZero() || end.IsZero() || !end.After(start) {
		return false
	}

	loc := parisLocation()
	startInLoc := start.In(loc)
	endInLoc := end.In(loc)

	dayStart := time.Date(startInLoc.Year(), startInLoc.Month(), startInLoc.Day(), 8, 0, 0, 0, loc)
	dayEnd := time.Date(startInLoc.Year(), startInLoc.Month(), startInLoc.Day(), 20, 0, 0, 0, loc)

	return !startInLoc.Before(dayStart) && !endInLoc.After(dayEnd)
}

func locationRetainedRanges(sessions []LocationSession, targetDay time.Time) []TimeRange {
	ranges := make([]TimeRange, 0, len(sessions))
	for _, session := range sessions {
		if !targetDay.IsZero() && (!isSameParisDay(session.BeginAt, targetDay) || !isSameParisDay(session.EndAt, targetDay)) {
			continue
		}
		if !IsCountedLocationSession(session) {
			continue
		}
		ranges = append(ranges, TimeRange{
			Start: session.BeginAt.In(parisLocation()),
			End:   session.EndAt.In(parisLocation()),
		})
	}
	return ranges
}

func mergeTimeRanges(ranges []TimeRange) []TimeRange {
	if len(ranges) == 0 {
		return nil
	}

	sorted := make([]TimeRange, len(ranges))
	copy(sorted, ranges)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Start.Equal(sorted[j].Start) {
			return sorted[i].End.Before(sorted[j].End)
		}
		return sorted[i].Start.Before(sorted[j].Start)
	})

	merged := []TimeRange{sorted[0]}
	for _, current := range sorted[1:] {
		lastIndex := len(merged) - 1
		last := merged[lastIndex]
		if !current.Start.After(last.End) {
			if current.End.After(last.End) {
				merged[lastIndex].End = current.End
			}
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

func sumTimeRanges(ranges []TimeRange) time.Duration {
	var total time.Duration
	for _, current := range ranges {
		if current.End.After(current.Start) {
			total += current.End.Sub(current.Start)
		}
	}
	return total
}

func LocationSessionsDuration(sessions []LocationSession) time.Duration {
	var total time.Duration
	for _, session := range sessions {
		if session.EndAt.After(session.BeginAt) {
			total += session.EndAt.Sub(session.BeginAt)
		}
	}
	return total
}

func CombinedRetainedDuration(badgeEvents []BadgeEvent, fallbackFirst, fallbackLast time.Time, locationSessions []LocationSession) time.Duration {
	targetDay, _ := retainedTargetDay(badgeEvents, fallbackFirst, fallbackLast, locationSessions)
	ranges := badgeRetainedRanges(badgeEvents, fallbackFirst, fallbackLast)
	ranges = append(ranges, locationRetainedRanges(locationSessions, targetDay)...)
	return sumTimeRanges(mergeTimeRanges(ranges))
}
