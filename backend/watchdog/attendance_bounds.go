package watchdog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"watchdog/config"

	apiManager "github.com/TheKrainBow/go-api"
)

const (
	attendanceAccessControlSource   = "access-control"
	attendanceBoundsWindowStartTime = "07:30:00"
	attendanceBoundsWindowEndTime   = "20:30:00"
)

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
	query := url.Values{}
	query.Set("page[size]", "1000")
	query.Set("begin_at", dayStart.UTC().Format(time.RFC3339))
	query.Set("end_at", dayEnd.UTC().Format(time.RFC3339))
	query.Set("time_begin_at", attendanceBoundsWindowStartTime)
	query.Set("time_end_at", attendanceBoundsWindowEndTime)
	query.Set("sources", attendanceAccessControlSource)
	query.Set("allow_overflow", "false")

	path := fmt.Sprintf("/users/%s/attendances?%s", url.PathEscape(login), query.Encode())
	resp, err := apiManager.GetClient(config.FTAttendance).Get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
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
		return nil, nil
	}

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
		return nil, nil
	}
	if source == "" {
		source = attendanceAccessControlSource
	}

	return &AttendanceBounds{
		BeginAt: beginAt,
		EndAt:   endAt,
		Source:  source,
	}, nil
}

func fetchAttendanceBoundsForMonth(login, monthKey string) (map[string]*AttendanceBounds, error) {
	loc := parisLocation()
	monthStart, err := time.ParseInLocation("2006-01", strings.TrimSpace(monthKey), loc)
	if err != nil {
		return nil, err
	}
	monthEnd := monthStart.AddDate(0, 1, 0).Add(-time.Nanosecond)

	query := url.Values{}
	query.Set("page[size]", "1000")
	query.Set("begin_at", monthStart.UTC().Format(time.RFC3339))
	query.Set("end_at", monthEnd.UTC().Format(time.RFC3339))
	query.Set("time_begin_at", attendanceBoundsWindowStartTime)
	query.Set("time_end_at", attendanceBoundsWindowEndTime)
	query.Set("sources", attendanceAccessControlSource)
	query.Set("allow_overflow", "false")

	path := fmt.Sprintf("/users/%s/attendances?%s", url.PathEscape(login), query.Encode())
	resp, err := apiManager.GetClient(config.FTAttendance).Get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
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
		return events
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

	return locationSessions, attendanceBounds, locationErr, attendanceErr
}
