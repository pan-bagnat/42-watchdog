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
	FetchedAt  time.Time
	Sessions   []LocationSession
	Refreshing bool
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

func fetchLocationSessionsForDay(login string, dayKey string) ([]LocationSession, error) {
	dayStart, dayEnd := locationSessionsBoundsForDay(dayKey)

	query := url.Values{}
	query.Set("sort", "begin_at")
	query.Set("page[size]", "100")
	query.Set("filter[campus_id]", strconv.Itoa(getCampusID()))
	query.Set("range[begin_at]", dayStart.UTC().Format(time.RFC3339)+","+dayEnd.UTC().Format(time.RFC3339))

	path := fmt.Sprintf("/users/%s/locations?%s", url.PathEscape(login), query.Encode())
	resp, err := apiManager.GetClient(config.FTv2).Get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

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

		if dayKey != todayDayKeyInParis() && item.EndAt == nil {
			endAt = dayEnd
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
	return sessions, nil
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
		dailyLocationSessionsMutex.Unlock()
		return sessions
	}
	staleSessions := []LocationSession(nil)
	if cached, ok := dailyLocationSessions[login]; ok {
		staleSessions = cloneLocationSessions(cached.Sessions)
	}
	dailyLocationSessionsMutex.Unlock()

	sessions, err := fetchDailyLocationSessions(login)
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not fetch daily locations for %s: %v", login, err))
		if len(staleSessions) > 0 {
			return staleSessions
		}
		return nil
	}

	dailyLocationSessionsMutex.Lock()
	defer dailyLocationSessionsMutex.Unlock()

	ensureDailyLocationSessionsLocked()
	dailyLocationSessions[login] = locationSessionsCacheEntry{
		FetchedAt: now,
		Sessions:  cloneLocationSessions(sessions),
	}
	return cloneLocationSessions(sessions)
}

func refreshDailyLocationSessionsAsync(login, dayKey string) {
	sessions, err := fetchLocationSessionsForDay(login, dayKey)
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
	} else {
		dailyLocationSessions[login] = locationSessionsCacheEntry{
			FetchedAt:  now,
			Sessions:   cloneLocationSessions(sessions),
			Refreshing: false,
		}
		shouldNotify = true
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
		dailyLocationSessionsMutex.Unlock()
		return sessions, false
	}

	staleSessions := cloneLocationSessions(entry.Sessions)
	if ok && entry.Refreshing {
		dailyLocationSessionsMutex.Unlock()
		return staleSessions, true
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
