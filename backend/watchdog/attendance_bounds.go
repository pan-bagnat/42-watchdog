package watchdog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"watchdog/config"

	apiManager "github.com/TheKrainBow/go-api"
)

const (
	attendanceAccessControlSource = "access-control"
	attendanceWindowStartHour     = 7
	attendanceWindowStartMinute   = 30
	attendanceWindowEndHour       = 20
	attendanceWindowEndMinute     = 30
)

// attendanceWindowUTCTimes converts the local (Paris) 07:30-20:30 attendance
// window into its UTC time-of-day equivalent for the specific calendar date of
// `reference`. Chronos' `time_begin_at`/`time_end_at` query params are matched
// against each record's raw (UTC) timestamps, so a fixed "07:30:00"/"20:30:00"
// string would silently drop records near the window edges whenever Paris is
// not on UTC+0 (e.g. CEST in summer shifts local 07:30 to 05:30 UTC).
func attendanceWindowUTCTimes(reference time.Time) (string, string) {
	loc := parisLocation()
	local := reference.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), attendanceWindowStartHour, attendanceWindowStartMinute, 0, 0, loc)
	end := time.Date(local.Year(), local.Month(), local.Day(), attendanceWindowEndHour, attendanceWindowEndMinute, 0, 0, loc)
	return start.UTC().Format("15:04:05"), end.UTC().Format("15:04:05")
}

type AttendanceBounds struct {
	BeginAt time.Time `json:"begin_at"`
	EndAt   time.Time `json:"end_at"`
	Source  string    `json:"source"`
}

type apiAttendanceResponse struct {
	Source  string `json:"source"`
	BeginAt string `json:"begin_at"`
	EndAt   string `json:"end_at"`
}

func cloneAttendanceBounds(bounds *AttendanceBounds) *AttendanceBounds {
	if bounds == nil {
		return nil
	}
	cloned := *bounds
	return &cloned
}

func attendanceBoundsForDay(dayKey string) (time.Time, time.Time) {
	loc := parisLocation()
	target := time.Now().In(loc)
	if strings.TrimSpace(dayKey) != "" {
		parsed, err := time.ParseInLocation("2006-01-02", dayKey, loc)
		if err == nil {
			target = parsed
		}
	}

	dayStart := time.Date(target.Year(), target.Month(), target.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.Add(24*time.Hour - time.Nanosecond)
	return dayStart, dayEnd
}

func fetchAttendanceBoundsForDay(login, dayKey string) (*AttendanceBounds, error) {
	dayStart, dayEnd := attendanceBoundsForDay(dayKey)
	return fetchAttendanceBoundsForRange(login, dayStart, dayEnd)
}

func fetchAttendanceBoundsForRange(login string, dayStart, dayEnd time.Time) (*AttendanceBounds, error) {
	windowStart, windowEnd := attendanceWindowUTCTimes(dayStart)
	query := url.Values{}
	query.Set("page[size]", "1000")
	query.Set("begin_at", dayStart.UTC().Format(time.RFC3339))
	query.Set("end_at", dayEnd.UTC().Format(time.RFC3339))
	query.Set("time_begin_at", windowStart)
	query.Set("time_end_at", windowEnd)
	query.Set("sources", attendanceAccessControlSource)
	query.Set("allow_overflow", "false")

	path := fmt.Sprintf("/users/%s/attendances?%s", url.PathEscape(login), query.Encode())
	Trace("API", "GET %s: login=%s window=%s", path, login, traceBounds(dayStart, dayEnd))
	resp, err := apiManager.GetClient(config.FTAttendance).Get(path)
	if err != nil {
		Trace("API", "GET %s: login=%s failed: %v", path, login, err)
		return nil, err
	}
	defer resp.Body.Close()
	Trace("API", "GET %s: login=%s status=%s", path, login, resp.Status)

	if resp.StatusCode == http.StatusNotFound {
		Trace("BUILD", "attendance bounds for %s on %s: no Chronos record (404)", login, traceBounds(dayStart, dayEnd))
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("42 Chronos API returned %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var payload []apiAttendanceResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		Trace("BUILD", "attendance bounds for %s on %s: empty payload", login, traceBounds(dayStart, dayEnd))
		return nil, nil
	}
	Trace("BUILD", "attendance payload for %s on %s: %d raw records", login, traceBounds(dayStart, dayEnd), len(payload))

	var (
		beginAt time.Time
		endAt   time.Time
		source  string
	)
	for _, item := range payload {
		itemBeginAt, err := parseAPITimestamp(item.BeginAt)
		if err != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: invalid attendance begin_at for %s: %v", login, err))
			continue
		}
		itemEndAt, err := parseAPITimestamp(item.EndAt)
		if err != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: invalid attendance end_at for %s: %v", login, err))
			continue
		}
		if !itemEndAt.After(itemBeginAt) {
			continue
		}
		if beginAt.IsZero() || itemBeginAt.Before(beginAt) {
			beginAt = itemBeginAt
		}
		if endAt.IsZero() || itemEndAt.After(endAt) {
			endAt = itemEndAt
		}
		if source == "" && strings.TrimSpace(item.Source) != "" {
			source = strings.TrimSpace(item.Source)
		}
	}

	if beginAt.IsZero() || endAt.IsZero() || !endAt.After(beginAt) {
		Trace("BUILD", "attendance bounds for %s on %s: payload could not produce valid bounds", login, traceBounds(dayStart, dayEnd))
		return nil, nil
	}
	if source == "" {
		source = attendanceAccessControlSource
	}

	bounds := &AttendanceBounds{
		BeginAt: beginAt,
		EndAt:   endAt,
		Source:  source,
	}
	Trace("BUILD", "attendance bounds built for %s: source=%s bounds=%s", login, bounds.Source, traceBounds(bounds.BeginAt, bounds.EndAt))
	return bounds, nil
}

// CFAAttendanceRecordsForDay returns the raw, non-collapsed attendance records Chronos
// has for a student on a given day, regardless of source. This reflects what CFA/other
// systems see as presence, as opposed to watchdog's own computed badge/logtime timeline.
func CFAAttendanceRecordsForDay(login, dayKey string) ([]AttendanceBounds, error) {
	return fetchCFAAttendanceRecords(login, dayKey)
}

func fetchCFAAttendanceRecords(login, dayKey string) ([]AttendanceBounds, error) {
	dayStart, dayEnd := attendanceBoundsForDay(dayKey)
	windowStart, windowEnd := attendanceWindowUTCTimes(dayStart)

	query := url.Values{}
	query.Set("page[size]", "1000")
	query.Set("begin_at", dayStart.UTC().Format(time.RFC3339))
	query.Set("end_at", dayEnd.UTC().Format(time.RFC3339))
	query.Set("time_begin_at", windowStart)
	query.Set("time_end_at", windowEnd)
	query.Set("allow_overflow", "false")

	path := fmt.Sprintf("/users/%s/attendances?%s", url.PathEscape(login), query.Encode())
	Trace("API", "GET %s: login=%s window=%s", path, login, traceBounds(dayStart, dayEnd))
	resp, err := apiManager.GetClient(config.FTAttendance).Get(path)
	if err != nil {
		Trace("API", "GET %s: login=%s failed: %v", path, login, err)
		return nil, err
	}
	defer resp.Body.Close()
	Trace("API", "GET %s: login=%s status=%s", path, login, resp.Status)

	if resp.StatusCode == http.StatusNotFound {
		Trace("BUILD", "CFA attendance records for %s on %s: no Chronos record (404)", login, traceBounds(dayStart, dayEnd))
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("42 Chronos API returned %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var payload []apiAttendanceResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	records := make([]AttendanceBounds, 0, len(payload))
	for _, item := range payload {
		itemBeginAt, err := parseAPITimestamp(item.BeginAt)
		if err != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: invalid CFA attendance begin_at for %s: %v", login, err))
			continue
		}
		itemEndAt, err := parseAPITimestamp(item.EndAt)
		if err != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: invalid CFA attendance end_at for %s: %v", login, err))
			continue
		}
		if !itemEndAt.After(itemBeginAt) {
			continue
		}
		records = append(records, AttendanceBounds{
			BeginAt: itemBeginAt,
			EndAt:   itemEndAt,
			Source:  strings.TrimSpace(item.Source),
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].BeginAt.Before(records[j].BeginAt) })

	Trace("BUILD", "CFA attendance records fetched for %s on %s: %d", login, dayKey, len(records))
	return records, nil
}

func fetchAttendanceBoundsForMonth(login, monthKey string) (map[string]*AttendanceBounds, error) {
	loc := parisLocation()
	monthStart, err := time.ParseInLocation("2006-01", strings.TrimSpace(monthKey), loc)
	if err != nil {
		return nil, err
	}
	monthEnd := monthStart.AddDate(0, 1, 0).Add(-time.Nanosecond)
	// Chronos accepts a single time_begin_at/time_end_at pair per request, so a DST
	// changeover mid-month (late March/October) can shift the window by an hour for
	// the days on the other side of it. Using monthStart's offset is a pragmatic
	// approximation given that API limitation.
	windowStart, windowEnd := attendanceWindowUTCTimes(monthStart)

	query := url.Values{}
	query.Set("page[size]", "1000")
	query.Set("begin_at", monthStart.UTC().Format(time.RFC3339))
	query.Set("end_at", monthEnd.UTC().Format(time.RFC3339))
	query.Set("time_begin_at", windowStart)
	query.Set("time_end_at", windowEnd)
	query.Set("sources", attendanceAccessControlSource)
	query.Set("allow_overflow", "false")

	path := fmt.Sprintf("/users/%s/attendances?%s", url.PathEscape(login), query.Encode())
	Trace("API", "GET %s: login=%s month=%s window=%s", path, login, monthKey, traceBounds(monthStart, monthEnd))
	resp, err := apiManager.GetClient(config.FTAttendance).Get(path)
	if err != nil {
		Trace("API", "GET %s: login=%s failed: %v", path, login, err)
		return nil, err
	}
	defer resp.Body.Close()
	Trace("API", "GET %s: login=%s status=%s", path, login, resp.Status)

	if resp.StatusCode == http.StatusNotFound {
		Trace("BUILD", "monthly attendance bounds for %s on %s: no Chronos records (404)", login, monthKey)
		return map[string]*AttendanceBounds{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("42 Chronos API returned %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var payload []apiAttendanceResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	Trace("BUILD", "monthly attendance payload for %s on %s: %d raw records", login, monthKey, len(payload))

	grouped := make(map[string]*AttendanceBounds)
	for _, item := range payload {
		itemBeginAt, err := parseAPITimestamp(item.BeginAt)
		if err != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: invalid attendance begin_at for %s: %v", login, err))
			continue
		}
		itemEndAt, err := parseAPITimestamp(item.EndAt)
		if err != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: invalid attendance end_at for %s: %v", login, err))
			continue
		}
		if !itemEndAt.After(itemBeginAt) {
			continue
		}

		dayKey := dayKeyInParis(itemBeginAt)
		if !strings.HasPrefix(dayKey, strings.TrimSpace(monthKey)+"-") {
			continue
		}

		current := grouped[dayKey]
		if current == nil {
			current = &AttendanceBounds{Source: strings.TrimSpace(item.Source)}
			grouped[dayKey] = current
		}
		if current.BeginAt.IsZero() || itemBeginAt.Before(current.BeginAt) {
			current.BeginAt = itemBeginAt
		}
		if current.EndAt.IsZero() || itemEndAt.After(current.EndAt) {
			current.EndAt = itemEndAt
		}
		if current.Source == "" && strings.TrimSpace(item.Source) != "" {
			current.Source = strings.TrimSpace(item.Source)
		}
	}

	for dayKey, bounds := range grouped {
		if bounds == nil || bounds.BeginAt.IsZero() || bounds.EndAt.IsZero() || !bounds.EndAt.After(bounds.BeginAt) {
			delete(grouped, dayKey)
			continue
		}
		if bounds.Source == "" {
			bounds.Source = attendanceAccessControlSource
		}
	}
	Trace("BUILD", "monthly attendance bounds built for %s on %s: %d days", login, monthKey, len(grouped))

	return grouped, nil
}

func attendanceBoundsAsBadgeEvents(bounds *AttendanceBounds) []BadgeEvent {
	if bounds == nil || bounds.BeginAt.IsZero() || bounds.EndAt.IsZero() {
		return nil
	}

	events := []BadgeEvent{{
		Timestamp: bounds.BeginAt,
		DoorName:  strings.TrimSpace(bounds.Source),
	}}
	if !bounds.EndAt.Equal(bounds.BeginAt) {
		events = append(events, BadgeEvent{
			Timestamp: bounds.EndAt,
			DoorName:  strings.TrimSpace(bounds.Source),
		})
	}
	return events
}

func applyAttendanceBoundsFallback(events []BadgeEvent, bounds *AttendanceBounds) []BadgeEvent {
	if len(events) > 0 {
		Trace("BUILD", "badge fallback skipped: %d badge events already available", len(events))
		return events
	}
	if bounds == nil {
		Trace("BUILD", "badge fallback unavailable: no badge events and no attendance bounds")
	} else {
		Trace("BUILD", "badge fallback applied from attendance bounds: %s source=%s", traceBounds(bounds.BeginAt, bounds.EndAt), bounds.Source)
	}
	return attendanceBoundsAsBadgeEvents(bounds)
}

func attendanceBoundsDuration(bounds *AttendanceBounds) time.Duration {
	if bounds == nil {
		return 0
	}
	return RetainedDuration(bounds.BeginAt, bounds.EndAt)
}

func fetchSupplementalDayData(login, dayKey string, needLocations bool, needAttendance bool) ([]LocationSession, *AttendanceBounds, error, error) {
	var (
		locationSessions []LocationSession
		attendanceBounds *AttendanceBounds
		locationErr      error
		attendanceErr    error
	)
	Trace("BUILD", "supplemental day fetch start for %s on %s: needLocations=%t needAttendance=%t", login, dayKey, needLocations, needAttendance)

	results := make(chan struct{}, 2)
	if needLocations {
		go func() {
			locationSessions, locationErr = fetchLocationSessionsForDay(login, dayKey)
			results <- struct{}{}
		}()
	}
	if needAttendance {
		go func() {
			attendanceBounds, attendanceErr = fetchAttendanceBoundsForDay(login, dayKey)
			results <- struct{}{}
		}()
	}

	pending := 0
	if needLocations {
		pending++
	}
	if needAttendance {
		pending++
	}
	for i := 0; i < pending; i++ {
		<-results
	}
	Trace("BUILD", "supplemental day fetch done for %s on %s: locations=%d attendance_bounds=%t locationErr=%v attendanceErr=%v", login, dayKey, len(locationSessions), attendanceBounds != nil, locationErr, attendanceErr)

	return locationSessions, attendanceBounds, locationErr, attendanceErr
}
