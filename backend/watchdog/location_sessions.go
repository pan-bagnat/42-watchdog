package watchdog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"watchdog/config"

	apiManager "github.com/TheKrainBow/go-api"
)

type apiLocationResponse struct {
	BeginAt string  `json:"begin_at"`
	EndAt   *string `json:"end_at"`
	Host    string  `json:"host"`
}

type LocationSession struct {
	BeginAt time.Time `json:"begin_at"`
	EndAt   time.Time `json:"end_at"`
	Host    string    `json:"host"`
	Ongoing bool      `json:"ongoing"`
}

type locationSessionsCacheEntry struct {
	FetchedAt        time.Time
	Sessions         []LocationSession
	AttendanceBounds *AttendanceBounds
	Refreshing       bool
}

var dailyLocationSessions = make(map[string]locationSessionsCacheEntry)
var dailyLocationSessionsDayKey string
var dailyLocationSessionsMutex sync.Mutex

const dailyLocationSessionsTTL = time.Minute

func ensureDailyLocationSessionsLocked() {
	todayKey := todayDayKeyInParis()
	if dailyLocationSessionsDayKey == todayKey {
		return
	}
	dailyLocationSessionsDayKey = todayKey
	dailyLocationSessions = make(map[string]locationSessionsCacheEntry)
}

func cloneLocationSessions(sessions []LocationSession) []LocationSession {
	if len(sessions) == 0 {
		return nil
	}
	out := make([]LocationSession, len(sessions))
	copy(out, sessions)
	return out
}

func dailyLocationSessionsBounds() (time.Time, time.Time) {
	return locationSessionsBoundsForDay(todayDayKeyInParis())
}

func locationSessionsBoundsForDay(dayKey string) (time.Time, time.Time) {
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

func locationSessionsBoundsForMonth(monthKey string) (time.Time, time.Time, error) {
	loc := parisLocation()
	start, err := time.ParseInLocation("2006-01", strings.TrimSpace(monthKey), loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end := start.AddDate(0, 1, 0).Add(-time.Nanosecond)
	return start, end, nil
}

func parseAPITimestamp(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}

	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, trimmed)
}

func fetchDailyLocationSessions(login string) ([]LocationSession, error) {
	return fetchLocationSessionsForDay(login, todayDayKeyInParis())
}

func fetchDailySupplementalData(login, dayKey string) ([]LocationSession, *AttendanceBounds, error, error) {
	badgeEvents, err := loadBadgeEventsForLogin(dayKey, login)
	if err != nil {
		Trace("BUILD", "daily supplemental data for %s on %s: badge lookup failed, fetching locations only before falling back to error: %v", login, dayKey, err)
		locationSessions, _, locationErr, _ := fetchSupplementalDayData(login, dayKey, true, false)
		return locationSessions, nil, locationErr, err
	}
	Trace("BUILD", "daily supplemental data for %s on %s: %d persisted badge events found, needAttendance=%t", login, dayKey, len(badgeEvents), len(badgeEvents) == 0)
	return fetchSupplementalDayData(login, dayKey, true, len(badgeEvents) == 0)
}

func fetchLocationSessionsForDay(login string, dayKey string) ([]LocationSession, error) {
	dayStart, dayEnd := locationSessionsBoundsForDay(dayKey)
	return fetchLocationSessionsForRange(login, dayStart, dayEnd)
}

func fetchLocationSessionsForRange(login string, dayStart, dayEnd time.Time) ([]LocationSession, error) {
	query := url.Values{}
	query.Set("sort", "begin_at")
	query.Set("page[size]", "1000")
	query.Set("filter[campus_id]", strconv.Itoa(getCampusID()))
	query.Set("range[begin_at]", dayStart.UTC().Format(time.RFC3339)+","+dayEnd.UTC().Format(time.RFC3339))

	path := fmt.Sprintf("/users/%s/locations?%s", url.PathEscape(login), query.Encode())
	Trace("API", "GET %s: login=%s window=%s", path, login, traceBounds(dayStart, dayEnd))
	resp, err := apiManager.GetClient(config.FTv2).Get(path)
	if err != nil {
		Trace("API", "GET %s: login=%s failed: %v", path, login, err)
		return nil, err
	}
	defer resp.Body.Close()
	Trace("API", "GET %s: login=%s status=%s", path, login, resp.Status)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("42 API returned %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var payload []apiLocationResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	Trace("BUILD", "locations payload for %s on %s: %d raw records", login, traceBounds(dayStart, dayEnd), len(payload))

	now := time.Now()
	sessions := make([]LocationSession, 0, len(payload))
	for _, item := range payload {
		beginAt, err := parseAPITimestamp(item.BeginAt)
		if err != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: invalid location begin_at for %s: %v", login, err))
			continue
		}

		endAt := now
		ongoing := true
		if item.EndAt != nil && strings.TrimSpace(*item.EndAt) != "" {
			endAt, err = parseAPITimestamp(*item.EndAt)
			if err != nil {
				Log(fmt.Sprintf("[WATCHDOG] WARNING: invalid location end_at for %s: %v", login, err))
				continue
			}
			ongoing = false
		}

		sessionDayEnd := time.Date(beginAt.In(parisLocation()).Year(), beginAt.In(parisLocation()).Month(), beginAt.In(parisLocation()).Day(), 23, 59, 59, int(time.Second-time.Nanosecond), parisLocation())
		if dayKeyInParis(beginAt) != todayDayKeyInParis() && item.EndAt == nil {
			endAt = sessionDayEnd
			ongoing = false
		}

		if endAt.After(dayEnd) {
			endAt = dayEnd
			ongoing = false
		}

		if !endAt.After(beginAt) {
			continue
		}

		if !timeRangesOverlap(beginAt, endAt, dayStart, dayEnd.Add(time.Nanosecond)) {
			continue
		}

		sessions = append(sessions, LocationSession{
			BeginAt: beginAt,
			EndAt:   endAt,
			Host:    strings.TrimSpace(item.Host),
			Ongoing: ongoing,
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].BeginAt.Before(sessions[j].BeginAt)
	})
	if len(sessions) == 0 {
		Trace("BUILD", "locations built for %s on %s: no retained sessions", login, traceBounds(dayStart, dayEnd))
	} else {
		Trace("BUILD", "locations built for %s on %s: %d sessions, first=%s, last=%s", login, traceBounds(dayStart, dayEnd), len(sessions), traceBounds(sessions[0].BeginAt, sessions[0].EndAt), traceBounds(sessions[len(sessions)-1].BeginAt, sessions[len(sessions)-1].EndAt))
	}
	return sessions, nil
}

func fetchLocationSessionsForMonth(login string, monthKey string) (map[string][]LocationSession, error) {
	monthStart, monthEnd, err := locationSessionsBoundsForMonth(monthKey)
	if err != nil {
		return nil, err
	}
	Trace("API", "monthly locations fetch requested for %s on %s (%s)", login, monthKey, traceBounds(monthStart, monthEnd))

	sessions, err := fetchLocationSessionsForRange(login, monthStart, monthEnd)
	if err != nil {
		return nil, err
	}

	grouped := make(map[string][]LocationSession)
	for _, session := range sessions {
		dayKey := dayKeyInParis(session.BeginAt)
		if !strings.HasPrefix(dayKey, strings.TrimSpace(monthKey)+"-") {
			continue
		}
		grouped[dayKey] = append(grouped[dayKey], session)
	}
	Trace("BUILD", "monthly locations built for %s on %s: %d days from %d sessions", login, monthKey, len(grouped), len(sessions))
	return grouped, nil
}

func SnapshotDailyLocationSessions(login string) []LocationSession {
	login = strings.ToLower(strings.TrimSpace(login))
	if login == "" {
		return nil
	}

	now := time.Now()

	dailyLocationSessionsMutex.Lock()
	ensureDailyLocationSessionsLocked()
	if cached, ok := dailyLocationSessions[login]; ok && now.Sub(cached.FetchedAt) < dailyLocationSessionsTTL {
		sessions := cloneLocationSessions(cached.Sessions)
		Trace("CACHE", "daily locations snapshot hit for %s: %d sessions cached_at=%s age=%s", login, len(sessions), traceTime(cached.FetchedAt), now.Sub(cached.FetchedAt).Round(time.Second))
		dailyLocationSessionsMutex.Unlock()
		return sessions
	}
	staleSessions := []LocationSession(nil)
	if cached, ok := dailyLocationSessions[login]; ok {
		staleSessions = cloneLocationSessions(cached.Sessions)
		Trace("CACHE", "daily locations snapshot stale for %s: %d sessions cached_at=%s age=%s", login, len(staleSessions), traceTime(cached.FetchedAt), now.Sub(cached.FetchedAt).Round(time.Second))
	} else {
		Trace("CACHE", "daily locations snapshot miss for %s", login)
	}
	dailyLocationSessionsMutex.Unlock()

	sessions, attendanceBounds, err, attendanceErr := fetchDailySupplementalData(login, todayDayKeyInParis())
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not fetch daily locations for %s: %v", login, err))
		if len(staleSessions) > 0 {
			return staleSessions
		}
		return nil
	}
	if attendanceErr != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not fetch Chronos attendance fallback for %s: %v", login, attendanceErr))
	}

	dailyLocationSessionsMutex.Lock()
	defer dailyLocationSessionsMutex.Unlock()

	ensureDailyLocationSessionsLocked()
	dailyLocationSessions[login] = locationSessionsCacheEntry{
		FetchedAt:        now,
		Sessions:         cloneLocationSessions(sessions),
		AttendanceBounds: cloneAttendanceBounds(attendanceBounds),
	}
	Trace("CACHE", "daily locations snapshot store for %s: %d sessions attendance_bounds=%t", login, len(sessions), attendanceBounds != nil)
	return cloneLocationSessions(sessions)
}

func refreshDailyLocationSessionsAsync(login, dayKey string) {
	Trace("CACHE", "async refresh start for %s on %s", login, dayKey)
	sessions, attendanceBounds, err, attendanceErr := fetchDailySupplementalData(login, dayKey)
	now := time.Now()
	shouldNotify := false

	dailyLocationSessionsMutex.Lock()
	ensureDailyLocationSessionsLocked()
	if dayKey != todayDayKeyInParis() {
		dailyLocationSessionsMutex.Unlock()
		return
	}

	entry := dailyLocationSessions[login]
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not refresh daily locations for %s: %v", login, err))
		entry.FetchedAt = now
		entry.Refreshing = false
		dailyLocationSessions[login] = entry
		shouldNotify = true
		Trace("CACHE", "async refresh failed for %s on %s, stale cache kept", login, dayKey)
	} else {
		if attendanceErr != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not refresh Chronos attendance fallback for %s: %v", login, attendanceErr))
			attendanceBounds = entry.AttendanceBounds
		}
		dailyLocationSessions[login] = locationSessionsCacheEntry{
			FetchedAt:        now,
			Sessions:         cloneLocationSessions(sessions),
			AttendanceBounds: cloneAttendanceBounds(attendanceBounds),
			Refreshing:       false,
		}
		shouldNotify = true
		Trace("CACHE", "async refresh stored for %s on %s: %d sessions attendance_bounds=%t", login, dayKey, len(sessions), attendanceBounds != nil)
	}
	dailyLocationSessionsMutex.Unlock()

	if shouldNotify {
		NotifyLocationSessionsUpdate(login, dayKey)
	}
}

func SnapshotDailyLocationSessionsOrSchedule(login string) ([]LocationSession, bool) {
	login = strings.ToLower(strings.TrimSpace(login))
	if login == "" {
		return nil, false
	}

	now := time.Now()
	dayKey := todayDayKeyInParis()

	dailyLocationSessionsMutex.Lock()
	ensureDailyLocationSessionsLocked()

	entry, ok := dailyLocationSessions[login]
	if ok && !entry.Refreshing && !entry.FetchedAt.IsZero() && now.Sub(entry.FetchedAt) < dailyLocationSessionsTTL {
		sessions := cloneLocationSessions(entry.Sessions)
		Trace("CACHE", "daily locations schedule hit for %s: %d sessions cached_at=%s age=%s", login, len(sessions), traceTime(entry.FetchedAt), now.Sub(entry.FetchedAt).Round(time.Second))
		dailyLocationSessionsMutex.Unlock()
		return sessions, false
	}

	staleSessions := cloneLocationSessions(entry.Sessions)
	if ok && entry.Refreshing {
		Trace("CACHE", "daily locations schedule refresh already running for %s: returning %d stale sessions", login, len(staleSessions))
		dailyLocationSessionsMutex.Unlock()
		return staleSessions, true
	}
	if ok {
		Trace("CACHE", "daily locations schedule stale for %s: returning %d stale sessions and starting refresh", login, len(staleSessions))
	} else {
		Trace("CACHE", "daily locations schedule miss for %s: starting refresh", login)
	}

	dailyLocationSessions[login] = locationSessionsCacheEntry{
		FetchedAt:  entry.FetchedAt,
		Sessions:   staleSessions,
		Refreshing: true,
	}
	dailyLocationSessionsMutex.Unlock()

	go refreshDailyLocationSessionsAsync(login, dayKey)
	return staleSessions, true
}

func SnapshotDailyAttendanceBoundsOrSchedule(login string) (*AttendanceBounds, bool) {
	login = strings.ToLower(strings.TrimSpace(login))
	if login == "" {
		return nil, false
	}

	now := time.Now()
	dayKey := todayDayKeyInParis()

	dailyLocationSessionsMutex.Lock()
	ensureDailyLocationSessionsLocked()

	entry, ok := dailyLocationSessions[login]
	if ok && !entry.Refreshing && !entry.FetchedAt.IsZero() && now.Sub(entry.FetchedAt) < dailyLocationSessionsTTL {
		bounds := cloneAttendanceBounds(entry.AttendanceBounds)
		Trace("CACHE", "daily attendance bounds hit for %s: cached_at=%s age=%s present=%t", login, traceTime(entry.FetchedAt), now.Sub(entry.FetchedAt).Round(time.Second), bounds != nil)
		dailyLocationSessionsMutex.Unlock()
		return bounds, false
	}

	staleBounds := cloneAttendanceBounds(entry.AttendanceBounds)
	if ok && entry.Refreshing {
		Trace("CACHE", "daily attendance bounds refresh already running for %s: present=%t", login, staleBounds != nil)
		dailyLocationSessionsMutex.Unlock()
		return staleBounds, true
	}
	if ok {
		Trace("CACHE", "daily attendance bounds stale for %s: present=%t, starting refresh", login, staleBounds != nil)
	} else {
		Trace("CACHE", "daily attendance bounds miss for %s: starting refresh", login)
	}

	dailyLocationSessions[login] = locationSessionsCacheEntry{
		FetchedAt:        entry.FetchedAt,
		Sessions:         cloneLocationSessions(entry.Sessions),
		AttendanceBounds: staleBounds,
		Refreshing:       true,
	}
	dailyLocationSessionsMutex.Unlock()

	go refreshDailyLocationSessionsAsync(login, dayKey)
	return staleBounds, true
}

func DeleteDailyLocationSessions(login string) {
	login = strings.ToLower(strings.TrimSpace(login))
	if login == "" {
		return
	}

	dailyLocationSessionsMutex.Lock()
	defer dailyLocationSessionsMutex.Unlock()

	ensureDailyLocationSessionsLocked()
	delete(dailyLocationSessions, login)
}
