package watchdog

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	apiManager "github.com/TheKrainBow/go-api"

	"watchdog/config"
)

const (
	DayTypeCompany       = "company"
	DayTypeWeekend       = "weekend"
	DayTypeOnSiteSchool  = "on_site_school"
	DayTypeOffSiteSchool = "off_site_school"
	DayTypeHoliday       = "holiday"
)

var errCFAClientUnavailable = errors.New("CFA API client is not initialized")

const cfaTrainingCalendarTTL = time.Hour

var (
	cfaTrainingCalendarCacheMu sync.Mutex
	cfaTrainingCalendarCache   = map[int]cfaTrainingCalendarCacheEntry{}
)

type cfaTrainingCalendarCacheEntry struct {
	FetchedAt time.Time
	Calendar  cfaTrainingCalendar
}

type StudentCalendarDay struct {
	DayType                 string   `json:"day_type"`
	DayTypeLabel            string   `json:"day_type_label"`
	RequiredAttendanceHours *float64 `json:"required_attendance_hours"`
}

type cfaApprenticeshipListResponse struct {
	Items []cfaApprenticeshipItem `json:"items"`
}

type cfaApprenticeshipItem struct {
	Status   string             `json:"status"`
	Training cfaTrainingSummary `json:"training"`
}

type cfaTrainingSummary struct {
	ID int `json:"id"`
}

type cfaTrainingCalendar struct {
	Dates map[string]cfaTrainingCalendarDate `json:"apprenticeship_dates"`
}

type cfaTrainingCalendarDate struct {
	Location                string   `json:"location"`
	RequiredAttendanceHours *float64 `json:"required_attendance_hours"`
}

func cfaDayTypeLabel(dayType string) string {
	switch strings.TrimSpace(dayType) {
	case DayTypeCompany:
		return "Jour entreprise"
	case DayTypeWeekend:
		return "Week-end"
	case DayTypeOffSiteSchool:
		return "Jour école à distance"
	case DayTypeHoliday:
		return "Jour férié"
	case DayTypeOnSiteSchool:
		fallthrough
	default:
		return "Jour école sur site"
	}
}

func inferredRequiredAttendanceHours(dayType string, raw *float64) *float64 {
	if raw != nil {
		value := *raw
		return &value
	}
	switch dayType {
	case DayTypeCompany, DayTypeWeekend, DayTypeHoliday:
		value := 0.0
		return &value
	default:
		return nil
	}
}

func defaultStudentCalendarDay(dayKey string) StudentCalendarDay {
	dayType := DayTypeOnSiteSchool
	if isWeekendDayKey(dayKey) {
		dayType = DayTypeWeekend
	}
	return StudentCalendarDay{
		DayType:                 dayType,
		DayTypeLabel:            cfaDayTypeLabel(dayType),
		RequiredAttendanceHours: inferredRequiredAttendanceHours(dayType, nil),
	}
}

func isWeekendDayKey(dayKey string) bool {
	parsed, err := time.ParseInLocation("2006-01-02", dayKey, parisLocation())
	if err != nil {
		return false
	}
	weekday := parsed.Weekday()
	return weekday == time.Saturday || weekday == time.Sunday
}

func buildDefaultStudentCalendarMonth(monthKey string) (map[string]StudentCalendarDay, error) {
	dayKeys, err := monthDayKeys(monthKey)
	if err != nil {
		return nil, err
	}
	out := make(map[string]StudentCalendarDay, len(dayKeys))
	for _, dayKey := range dayKeys {
		out[dayKey] = defaultStudentCalendarDay(dayKey)
	}
	Trace("BUILD", "default student calendar built for month %s: %d days", monthKey, len(out))
	return out, nil
}

func loadStudentCalendarMonth(login, monthKey string) (map[string]StudentCalendarDay, error) {
	calendar, err := buildDefaultStudentCalendarMonth(monthKey)
	if err != nil {
		return nil, err
	}

	client := apiManager.GetClient(config.FTCFA)
	if client == nil {
		Trace("CACHE", "CFA calendar for %s on %s: client unavailable, using default calendar only", login, monthKey)
		return calendar, nil
	}

	trainingID, found, err := loadCFATrainingID(login, monthKey)
	if err != nil {
		return calendar, err
	}
	if !found {
		Trace("BUILD", "CFA calendar for %s on %s: no training found, using default calendar only", login, monthKey)
		return calendar, nil
	}

	trainingCalendar, err := loadCFATrainingCalendar(trainingID)
	if err != nil {
		return calendar, err
	}

	mergedDays := 0
	for dayKey, date := range trainingCalendar.Dates {
		if !strings.HasPrefix(dayKey, monthKey+"-") {
			continue
		}
		dayType := strings.TrimSpace(date.Location)
		if dayType == "" {
			dayType = calendar[dayKey].DayType
		}
		calendar[dayKey] = StudentCalendarDay{
			DayType:                 dayType,
			DayTypeLabel:            cfaDayTypeLabel(dayType),
			RequiredAttendanceHours: inferredRequiredAttendanceHours(dayType, date.RequiredAttendanceHours),
		}
		mergedDays++
	}
	Trace("BUILD", "CFA calendar merged for %s on %s: training_id=%d overridden_days=%d total_days=%d", login, monthKey, trainingID, mergedDays, len(calendar))

	return calendar, nil
}

func loadCFATrainingID(login, monthKey string) (int, bool, error) {
	if trainingID, found, ok, err := loadPersistedCFATrainingID(login, monthKey); err != nil {
		return 0, false, err
	} else if ok {
		Trace("CACHE", "CFA training id cache hit for %s on %s: training_id=%d found=%t", normalizeLogin(login), monthKey, trainingID, found)
		return trainingID, found, nil
	}
	Trace("CACHE", "CFA training id cache miss for %s on %s", normalizeLogin(login), monthKey)

	var response cfaApprenticeshipListResponse
	path := fmt.Sprintf("/apprenticeship/list?student_login=%s", url.QueryEscape(normalizeLogin(login)))
	if err := cfaGetJSON(path, &response); err != nil {
		return 0, false, err
	}

	for _, item := range response.Items {
		if strings.EqualFold(strings.TrimSpace(item.Status), "active") && item.Training.ID > 0 {
			if err := savePersistedCFATrainingID(login, item.Training.ID, monthKey); err != nil {
				return 0, false, err
			}
			Trace("BUILD", "CFA training id selected for %s on %s: active training_id=%d", normalizeLogin(login), monthKey, item.Training.ID)
			return item.Training.ID, true, nil
		}
	}
	for _, item := range response.Items {
		if item.Training.ID > 0 {
			if err := savePersistedCFATrainingID(login, item.Training.ID, monthKey); err != nil {
				return 0, false, err
			}
			Trace("BUILD", "CFA training id selected for %s on %s: fallback training_id=%d", normalizeLogin(login), monthKey, item.Training.ID)
			return item.Training.ID, true, nil
		}
	}
	if err := savePersistedCFATrainingID(login, 0, monthKey); err != nil {
		return 0, false, err
	}
	Trace("BUILD", "CFA training id selected for %s on %s: none", normalizeLogin(login), monthKey)
	return 0, false, nil
}

func loadCFATrainingCalendar(trainingID int) (cfaTrainingCalendar, error) {
	if cached, ok := loadCachedCFATrainingCalendar(trainingID); ok {
		Trace("CACHE", "CFA training calendar cache hit for training_id=%d: %d dates", trainingID, len(cached.Dates))
		return cached, nil
	}
	Trace("CACHE", "CFA training calendar cache miss for training_id=%d", trainingID)

	var response cfaTrainingCalendar
	if err := cfaGetJSON(fmt.Sprintf("/training/%d", trainingID), &response); err != nil {
		return cfaTrainingCalendar{}, err
	}
	saveCachedCFATrainingCalendar(trainingID, response)
	Trace("BUILD", "CFA training calendar fetched for training_id=%d: %d dates", trainingID, len(response.Dates))
	return response, nil
}

func loadCachedCFATrainingCalendar(trainingID int) (cfaTrainingCalendar, bool) {
	if trainingID <= 0 {
		return cfaTrainingCalendar{}, false
	}

	cfaTrainingCalendarCacheMu.Lock()
	defer cfaTrainingCalendarCacheMu.Unlock()

	entry, ok := cfaTrainingCalendarCache[trainingID]
	if !ok {
		return cfaTrainingCalendar{}, false
	}
	if time.Since(entry.FetchedAt) >= cfaTrainingCalendarTTL {
		delete(cfaTrainingCalendarCache, trainingID)
		Trace("CACHE", "CFA training calendar cache expired for training_id=%d", trainingID)
		return cfaTrainingCalendar{}, false
	}
	return entry.Calendar, true
}

func saveCachedCFATrainingCalendar(trainingID int, calendar cfaTrainingCalendar) {
	if trainingID <= 0 {
		return
	}

	cfaTrainingCalendarCacheMu.Lock()
	defer cfaTrainingCalendarCacheMu.Unlock()

	cfaTrainingCalendarCache[trainingID] = cfaTrainingCalendarCacheEntry{
		FetchedAt: time.Now(),
		Calendar:  calendar,
	}
	Trace("CACHE", "CFA training calendar cache store for training_id=%d: %d dates", trainingID, len(calendar.Dates))
}

func loadPersistedCFATrainingID(login, monthKey string) (int, bool, bool, error) {
	if storageDB == nil {
		return 0, false, false, nil
	}

	login = normalizeLogin(login)
	monthKey = strings.TrimSpace(monthKey)
	if login == "" || monthKey == "" {
		return 0, false, false, nil
	}

	var trainingID int
	var refreshedMonth string
	err := storageQueryRow(`
		SELECT training_id, refreshed_month
		FROM watchdog_cfa_training_ids
		WHERE login_42 = ?
	`, login).Scan(&trainingID, &refreshedMonth)
	if errors.Is(err, sql.ErrNoRows) {
		Trace("CACHE", "persisted CFA training id miss for %s on %s", login, monthKey)
		return 0, false, false, nil
	}
	if err != nil {
		return 0, false, false, err
	}
	if strings.TrimSpace(refreshedMonth) != monthKey {
		Trace("CACHE", "persisted CFA training id stale for %s: stored_month=%s requested_month=%s", login, strings.TrimSpace(refreshedMonth), monthKey)
		return 0, false, false, nil
	}
	Trace("CACHE", "persisted CFA training id hit for %s on %s: training_id=%d", login, monthKey, trainingID)
	return trainingID, trainingID > 0, true, nil
}

func savePersistedCFATrainingID(login string, trainingID int, monthKey string) error {
	if storageDB == nil {
		return nil
	}

	login = normalizeLogin(login)
	monthKey = strings.TrimSpace(monthKey)
	if login == "" || monthKey == "" {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := storageExec(`
		INSERT INTO watchdog_cfa_training_ids (
			login_42, training_id, refreshed_month, updated_at
		) VALUES (?, ?, ?, ?)
		ON CONFLICT(login_42) DO UPDATE SET
			training_id = excluded.training_id,
			refreshed_month = excluded.refreshed_month,
			updated_at = excluded.updated_at
	`, login, trainingID, monthKey, now)
	if err == nil {
		Trace("CACHE", "persisted CFA training id store for %s on %s: training_id=%d", login, monthKey, trainingID)
	}
	return err
}

func cfaGetJSON(path string, out any) error {
	client := apiManager.GetClient(config.FTCFA)
	if client == nil {
		return errCFAClientUnavailable
	}

	Trace("API", "GET %s: client=%s", path, config.FTCFA)
	resp, err := client.Get(path)
	if err != nil {
		Trace("API", "GET %s: client=%s failed: %v", path, config.FTCFA, err)
		return fmt.Errorf("CFA API GET %s: %w", path, err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
	Trace("API", "GET %s: client=%s status=%d", path, config.FTCFA, resp.StatusCode)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("CFA API GET %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("CFA API decode %s: %w", path, err)
	}
	Trace("BUILD", "CFA payload decoded for %s", path)

	return nil
}
