package watchdog

import (
	"fmt"
	"net/http"
	"strings"
	"time"
	"watchdog/config"

	apiManager "github.com/TheKrainBow/go-api"
)

// resolveUserIdentityForDay resolves the 42 identity (id_42, control_access_id)
// a student had on a given day, so a manual attendance post can be built without
// trusting client-supplied identity fields. It prefers the live/current-day
// record when dayKey is today, and falls back to the persisted historical record
// otherwise.
func resolveUserIdentityForDay(login, dayKey string) (User, error) {
	login = normalizeLogin(login)
	if login == "" {
		return User{}, fmt.Errorf("login is required")
	}

	if dayKey == currentRuntimeDayKey() {
		user, ok, err := CurrentUserByLogin(login)
		if err != nil {
			return User{}, err
		}
		if ok && strings.TrimSpace(user.ID42) != "" {
			return user, nil
		}
	}

	record, ok, err := HistoricalStudentDayByLogin(login, dayKey)
	if err != nil {
		return User{}, err
	}
	if !ok || strings.TrimSpace(record.User.ID42) == "" {
		return User{}, fmt.Errorf("could not resolve 42 identity for %s on %s", login, dayKey)
	}
	return record.User, nil
}

// combineDateAndClock builds a Paris-local instant from a calendar date and a
// clock string ("HH:MM" or "HH:MM:SS"), letting time.Date resolve the correct
// UTC offset for that specific date (handles CET/CEST automatically).
func combineDateAndClock(day time.Time, clock string, loc *time.Location) (time.Time, error) {
	trimmed := strings.TrimSpace(clock)
	parsed, err := time.Parse("15:04:05", trimmed)
	if err != nil {
		parsed, err = time.Parse("15:04", trimmed)
		if err != nil {
			return time.Time{}, fmt.Errorf("expected HH:MM or HH:MM:SS, got %q", clock)
		}
	}
	return time.Date(day.Year(), day.Month(), day.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, loc), nil
}

// PostManualAttendanceForDay parses a day key and begin/end clock strings (Paris
// local time) and posts a manual attendance record for that student/day.
func PostManualAttendanceForDay(login, dayKey, beginClock, endClock string) (AttendancePostRecord, error) {
	loc := parisLocation()
	dayDate, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dayKey), loc)
	if err != nil {
		return AttendancePostRecord{}, fmt.Errorf("invalid day %q: %w", dayKey, err)
	}

	beginAt, err := combineDateAndClock(dayDate, beginClock, loc)
	if err != nil {
		return AttendancePostRecord{}, fmt.Errorf("invalid begin_at: %w", err)
	}
	endAt, err := combineDateAndClock(dayDate, endClock, loc)
	if err != nil {
		return AttendancePostRecord{}, fmt.Errorf("invalid end_at: %w", err)
	}

	return PostManualAttendance(login, strings.TrimSpace(dayKey), beginAt, endAt)
}

// PostManualAttendance posts an admin-specified attendance range for a student
// straight to the 42 Chronos API, bypassing the automated batch's blacklist/
// AutoPost/school-day gating since this is an explicit, single-shot admin
// correction rather than the daily automated post. The attempt (success or
// failure) is always persisted for audit, exactly like the automated posts.
func PostManualAttendance(login, dayKey string, beginAt, endAt time.Time) (AttendancePostRecord, error) {
	login = normalizeLogin(login)
	if login == "" {
		return AttendancePostRecord{}, fmt.Errorf("login is required")
	}
	if !endAt.After(beginAt) {
		return AttendancePostRecord{}, fmt.Errorf("end time must be after start time")
	}

	user, err := resolveUserIdentityForDay(login, dayKey)
	if err != nil {
		return AttendancePostRecord{}, err
	}

	user.FirstAccess = beginAt
	user.LastAccess = endAt
	payload, err := buildAttendancePayload(user)
	if err != nil {
		return AttendancePostRecord{}, err
	}

	beginAtCopy := beginAt
	endAtCopy := endAt
	record := AttendancePostRecord{
		DayKey:          dayKey,
		Login42:         user.Login42,
		ID42:            user.ID42,
		ControlAccessID: user.ControlAccessID,
		BeginAt:         &beginAtCopy,
		EndAt:           &endAtCopy,
		Payload:         payload,
		CreatedAt:       time.Now().UTC(),
	}

	resp, postErr := apiManager.GetClient(config.FTAttendance).Post("/attendances", payload)
	if postErr != nil {
		record.ErrorMessage = postErr.Error()
		record.Success = false
		if persistErr := recordAttendancePost(dayKey, user, payload, nil, "", record.ErrorMessage, false); persistErr != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not persist manual attendance post for %s: %v", login, persistErr))
		}
		return record, postErr
	}
	defer resp.Body.Close()

	statusCode := resp.StatusCode
	record.HTTPStatus = &statusCode
	record.ResponseStatus = resp.Status

	if resp.StatusCode != http.StatusOK {
		record.ErrorMessage = resp.Status
		record.Success = false
		if persistErr := recordAttendancePost(dayKey, user, payload, &statusCode, resp.Status, record.ErrorMessage, false); persistErr != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not persist manual attendance post for %s: %v", login, persistErr))
		}
		return record, fmt.Errorf("42 Chronos API returned %s", resp.Status)
	}

	record.Success = true
	if persistErr := recordAttendancePost(dayKey, user, payload, &statusCode, resp.Status, "", true); persistErr != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not persist manual attendance post for %s: %v", login, persistErr))
	}
	loc := parisLocation()
	Log(fmt.Sprintf("[WATCHDOG] [MANUAL POST] ✅ %s: %s -> %s posted manually by an admin for %s", login, beginAt.In(loc).Format("15:04:05"), endAt.In(loc).Format("15:04:05"), dayKey))
	return record, nil
}
