package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"watchdog/watchdog"
)

type apiUserState struct {
	ControlAccessID   int                 `json:"control_access_id"`
	ControlAccessName string              `json:"control_access_name"`
	Login42           string              `json:"login_42"`
	ID42              string              `json:"id_42"`
	IsApprentice      bool                `json:"is_apprentice"`
	Status            string              `json:"status"`
	PostResult        string              `json:"post_result,omitempty"`
	Status42          string              `json:"status_42"`
	StatusOverridden  bool                `json:"status_overridden"`
	IsBlacklisted     bool                `json:"is_blacklisted"`
	BadgePostingOff   bool                `json:"badge_posting_off"`
	BlacklistReason   string              `json:"blacklist_reason,omitempty"`
	Profile           string              `json:"profile"`
	FirstAccess       *time.Time          `json:"first_access,omitempty"`
	LastAccess        *time.Time          `json:"last_access,omitempty"`
	DurationSeconds   int64               `json:"duration_seconds"`
	DurationHuman     string              `json:"duration_human"`
	ErrorMessage      string              `json:"error_message,omitempty"`
	AttendancePosts   []apiAttendancePost `json:"attendance_posts,omitempty"`
}

type apiBadgeEvent struct {
	Timestamp time.Time `json:"timestamp"`
	DoorName  string    `json:"door_name"`
}

type apiLocationSession struct {
	BeginAt time.Time `json:"begin_at"`
	EndAt   time.Time `json:"end_at"`
	Host    string    `json:"host"`
	Counted bool      `json:"counted"`
	Ongoing bool      `json:"ongoing"`
}

type apiAttendancePost struct {
	BeginAt        *time.Time             `json:"begin_at,omitempty"`
	EndAt          *time.Time             `json:"end_at,omitempty"`
	HTTPStatus     *int                   `json:"http_status,omitempty"`
	ResponseStatus string                 `json:"response_status,omitempty"`
	ErrorMessage   string                 `json:"error_message,omitempty"`
	Success        bool                   `json:"success"`
	CreatedAt      time.Time              `json:"created_at"`
	Payload        watchdog.APIAttendance `json:"payload"`
}

type apiStudentMeResponse struct {
	Day              string               `json:"day"`
	Live             bool                 `json:"live"`
	Login            string               `json:"login"`
	Tracked          bool                 `json:"tracked"`
	LocationsLoading bool                 `json:"locations_loading,omitempty"`
	User             *apiUserState        `json:"user,omitempty"`
	BadgeEvents      []apiBadgeEvent      `json:"badge_events"`
	LocationSessions []apiLocationSession `json:"location_sessions"`
	AttendancePosts  []apiAttendancePost  `json:"attendance_posts"`
}

type apiStudentUpdateRequest struct {
	Status           *string `json:"status,omitempty"`
	StatusOverridden *bool   `json:"status_overridden,omitempty"`
	IsBlacklisted    *bool   `json:"is_blacklisted,omitempty"`
	BlacklistReason  *string `json:"blacklist_reason,omitempty"`
}

type apiMessageResponse struct {
	Message string `json:"message"`
}

type apiAdminCalendarDay struct {
	Day          string `json:"day"`
	StudentCount int    `json:"student_count"`
	Live         bool   `json:"live"`
}

type apiAdminStudentDaysResponse struct {
	Login string               `json:"login"`
	Days  []apiAdminStudentDay `json:"days"`
}

type apiAdminStudentDay struct {
	Day             string     `json:"day"`
	Live            bool       `json:"live"`
	FirstAccess     *time.Time `json:"first_access,omitempty"`
	LastAccess      *time.Time `json:"last_access,omitempty"`
	DurationSeconds int64      `json:"duration_seconds"`
	DurationHuman   string     `json:"duration_human"`
	Status          string     `json:"status,omitempty"`
	Loading         bool       `json:"loading,omitempty"`
}

type apiAdminReportSummary struct {
	Day          string `json:"day"`
	StudentCount int    `json:"student_count"`
	PostedCount  int    `json:"posted_count"`
	FailedCount  int    `json:"failed_count"`
	Live         bool   `json:"live"`
}

type apiAdminUserListItem struct {
	Login42                     string     `json:"login_42"`
	PhotoURL                    string     `json:"photo_url,omitempty"`
	Status                      string     `json:"status"`
	Status42                    string     `json:"status_42"`
	StatusOverridden            bool       `json:"status_overridden"`
	IsBlacklisted               bool       `json:"is_blacklisted"`
	BadgePostingOff             bool       `json:"badge_posting_off"`
	BlacklistReason             string     `json:"blacklist_reason,omitempty"`
	LastBadgeAt                 *time.Time `json:"last_badge_at,omitempty"`
	LastBadgeDayDurationSeconds int64      `json:"last_badge_day_duration_seconds"`
	LastBadgeDayDurationHuman   string     `json:"last_badge_day_duration_human"`
}

type apiAdminUserDetailResponse struct {
	apiAdminUserListItem
	Days []apiAdminStudentDay `json:"days"`
}

func studentDetailHandler(w http.ResponseWriter, r *http.Request) {
	authUser := getAuthenticatedUser(r)
	if authUser == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	login := strings.ToLower(strings.TrimSpace(authUser.FtLogin))
	if login == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_login", "Authenticated login is missing.")
		return
	}

	switch r.Method {
	case http.MethodGet:
		monthKey, err := requestedMonthKey(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_month", "month must use YYYY-MM format.")
			return
		}

		days, err := watchdog.AttendanceDaysForLogin(login, monthKey)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "student_days_load_failed", "Could not load student days.")
			return
		}

		user, ok, err := watchdog.AdminUserByLogin(login)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "student_load_failed", "Could not load student.")
			return
		}

		payload := apiAdminUserDetailResponse{
			apiAdminUserListItem: apiAdminUserListItem{
				Login42:          login,
				PhotoURL:         strings.TrimSpace(authUser.PhotoURL),
				Status:           "student",
				Status42:         "student",
				StatusOverridden: false,
				IsBlacklisted:    false,
				BadgePostingOff:  false,
			},
			Days: make([]apiAdminStudentDay, 0, len(days)),
		}

		if ok {
			payload.apiAdminUserListItem = mapAdminUser(user)
			if payload.PhotoURL == "" {
				payload.PhotoURL = strings.TrimSpace(authUser.PhotoURL)
			}
		} else if settings, settingsOK, settingsErr := watchdog.UserSettingsByLogin(login); settingsErr == nil && settingsOK {
			if settings.Status != "" {
				payload.Status = settings.Status
			}
			if settings.Status42 != "" {
				payload.Status42 = settings.Status42
			}
			payload.StatusOverridden = settings.StatusOverridden
			payload.IsBlacklisted = settings.IsBlacklisted
			payload.BadgePostingOff = settings.BadgePostingOff
			payload.BlacklistReason = settings.BlacklistReason
		}

		for _, day := range days {
			payload.Days = append(payload.Days, apiAdminStudentDay{
				Day:             day.DayKey,
				Live:            day.Live,
				FirstAccess:     timePtr(day.FirstAccess),
				LastAccess:      timePtr(day.LastAccess),
				DurationSeconds: int64(day.Duration / time.Second),
				DurationHuman:   day.Duration.String(),
				Status:          day.Status,
				Loading:         day.Loading,
			})
		}

		writeJSON(w, http.StatusOK, payload)
	case http.MethodPatch:
		writeJSONError(w, http.StatusForbidden, "forbidden_patch", "Self-service badgeuse settings are disabled.")
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func adminStudentsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		dayKey, _, err := requestedDayKey(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_date", "date must use YYYY-MM-DD format.")
			return
		}
		apprenticesOnly := false
		if raw := strings.TrimSpace(r.URL.Query().Get("apprentices_only")); raw != "" {
			parsed, parseErr := strconv.ParseBool(raw)
			if parseErr != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_apprentices_only", "apprentices_only must be a boolean.")
				return
			}
			apprenticesOnly = parsed
		}
		users, _, err := watchdog.UsersForDay(dayKey, apprenticesOnly)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "students_load_failed", "Could not load watchdog state.")
			return
		}
		postsByLogin, err := watchdog.AttendancePostsForDay(dayKey)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "students_load_failed", "Could not load attendance posts.")
			return
		}
		writeJSON(w, http.StatusOK, mapUsersWithAttendancePosts(users, postsByLogin))
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func adminUsersHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		search := strings.TrimSpace(r.URL.Query().Get("search"))
		statuses := r.URL.Query()["status"]
		dayKey := strings.TrimSpace(r.URL.Query().Get("date"))
		if dayKey != "" {
			loc, err := time.LoadLocation("Europe/Paris")
			if err != nil {
				loc = time.Local
			}
			parsed, err := time.ParseInLocation("2006-01-02", dayKey, loc)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_date", "date must use YYYY-MM-DD format.")
				return
			}
			dayKey = parsed.Format("2006-01-02")
		}
		users, err := watchdog.AdminUsers(search, statuses, dayKey)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "users_load_failed", "Could not load admin users.")
			return
		}
		writeJSON(w, http.StatusOK, mapAdminUsers(users))
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func adminUserDetailHandler(w http.ResponseWriter, r *http.Request) {
	login := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/admin/users/"))
	if login == "" || strings.Contains(login, "/") {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		user, ok, err := watchdog.AdminUserByLogin(login)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "user_load_failed", "Could not load admin user.")
			return
		}
		if !ok {
			writeJSONError(w, http.StatusNotFound, "user_not_found", "User not found.")
			return
		}

		monthKey, err := requestedMonthKey(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_month", "month must use YYYY-MM format.")
			return
		}

		days, err := watchdog.AttendanceDaysForLogin(login, monthKey)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "user_days_load_failed", "Could not load user attendance days.")
			return
		}

		writeJSON(w, http.StatusOK, mapAdminUserDetail(user, days))
	case http.MethodPatch:
		var req apiStudentUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", "Request body must be valid JSON.")
			return
		}
		if req.Status != nil {
			if normalized := watchdog.AdminUserStatusFromInput(*req.Status); normalized == "" {
				writeJSONError(w, http.StatusBadRequest, "invalid_status", "status must be one of student, apprentice, pisciner, staff or extern.")
				return
			}
		}
		if _, ok, err := watchdog.AdminUserByLogin(login); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "user_load_failed", "Could not load admin user.")
			return
		} else if !ok {
			writeJSONError(w, http.StatusNotFound, "user_not_found", "User not found.")
			return
		}

		if req.Status == nil && req.StatusOverridden == nil && req.IsBlacklisted == nil && req.BlacklistReason == nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_patch", "Provide status, status_overridden, is_blacklisted or blacklist_reason.")
			return
		}

		if _, err := watchdog.UpdateUserSettings(login, watchdog.UserSettingsPatch{
			IsBlacklisted:    req.IsBlacklisted,
			BlacklistReason:  req.BlacklistReason,
			Status:           req.Status,
			StatusOverridden: req.StatusOverridden,
		}); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "settings_update_failed", "Could not update user settings.")
			return
		}

		user, ok, err := watchdog.AdminUserByLogin(login)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "user_load_failed", "Could not load admin user.")
			return
		}
		if !ok {
			writeJSONError(w, http.StatusNotFound, "user_not_found", "User not found.")
			return
		}
		writeJSON(w, http.StatusOK, mapAdminUser(user))
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func adminCalendarHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	monthKey, err := requestedMonthKey(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_month", "month must use YYYY-MM format.")
		return
	}

	days, err := watchdog.DayAvailabilityForMonth(monthKey)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "calendar_load_failed", "Could not load admin calendar.")
		return
	}

	payload := make([]apiAdminCalendarDay, 0, len(days))
	for _, day := range days {
		payload = append(payload, apiAdminCalendarDay{
			Day:          day.DayKey,
			StudentCount: day.StudentCount,
			Live:         day.Live,
		})
	}
	writeJSON(w, http.StatusOK, payload)
}

func adminReportsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	reports, err := watchdog.DailyReportSummaries()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "reports_load_failed", "Could not load daily reports.")
		return
	}

	payload := make([]apiAdminReportSummary, 0, len(reports))
	for _, report := range reports {
		payload = append(payload, apiAdminReportSummary{
			Day:          report.DayKey,
			StudentCount: report.StudentCount,
			PostedCount:  report.PostedCount,
			FailedCount:  report.FailedCount,
			Live:         report.Live,
		})
	}
	writeJSON(w, http.StatusOK, payload)
}

func adminStudentDaysHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	login := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/admin/student-days/"))
	if login == "" || strings.Contains(login, "/") {
		http.NotFound(w, r)
		return
	}

	monthKey, err := requestedMonthKey(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_month", "month must use YYYY-MM format.")
		return
	}

	days, err := watchdog.AttendanceDaysForLogin(login, monthKey)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "student_days_load_failed", "Could not load student days.")
		return
	}

	user, ok, err := watchdog.AdminUserByLogin(login)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "student_load_failed", "Could not load student.")
		return
	}
	if !ok {
		writeJSONError(w, http.StatusNotFound, "student_not_found", "Student not found.")
		return
	}

	payload := apiAdminStudentDaysResponse{
		Login: strings.ToLower(user.Login42),
		Days:  make([]apiAdminStudentDay, 0, len(days)),
	}
	for _, day := range days {
		payload.Days = append(payload.Days, apiAdminStudentDay{
			Day:             day.DayKey,
			Live:            day.Live,
			FirstAccess:     timePtr(day.FirstAccess),
			LastAccess:      timePtr(day.LastAccess),
			DurationSeconds: int64(day.Duration / time.Second),
			DurationHuman:   day.Duration.String(),
			Status:          day.Status,
			Loading:         day.Loading,
		})
	}
	writeJSON(w, http.StatusOK, payload)
}

func adminStudentHandler(w http.ResponseWriter, r *http.Request) {
	login := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/admin/students/"))
	if login == "" || strings.Contains(login, "/") {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if dayKey, historical, err := requestedDayKey(r); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_date", "date must use YYYY-MM-DD format.")
			return
		} else if historical {
			record, ok, err := watchdog.HistoricalStudentDayByLogin(login, dayKey)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "history_load_failed", "Could not load stored history.")
				return
			}
			if !ok {
				writeJSONError(w, http.StatusNotFound, "student_not_found", "Student history not found for that day.")
				return
			}
			writeJSON(w, http.StatusOK, mapHistoricalStudentResponse(record))
			return
		}
		user, ok, err := watchdog.CurrentUserByLogin(login)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "student_load_failed", "Could not load watchdog state.")
			return
		}
		badgeEvents := watchdog.SnapshotDailyBadgeEvents(login)
		locationSessions, locationsLoading := watchdog.SnapshotDailyLocationSessionsOrSchedule(login)
		if !ok {
			writeJSON(w, http.StatusOK, apiStudentMeResponse{
				Day:              todayDayKey(),
				Live:             true,
				Login:            login,
				Tracked:          false,
				LocationsLoading: locationsLoading,
				BadgeEvents:      mapBadgeEvents(badgeEvents),
				LocationSessions: mapLocationSessions(locationSessions),
				AttendancePosts:  []apiAttendancePost{},
			})
			return
		}
		retainedDuration := watchdog.CombinedRetainedDuration(
			badgeEvents,
			user.FirstAccess,
			user.LastAccess,
			locationSessions,
		)
		writeJSON(w, http.StatusOK, apiStudentMeResponse{
			Day:              todayDayKey(),
			Live:             true,
			Login:            login,
			Tracked:          true,
			LocationsLoading: locationsLoading,
			User:             mapUserWithDuration(user, retainedDuration),
			BadgeEvents:      mapBadgeEvents(badgeEvents),
			LocationSessions: mapLocationSessions(locationSessions),
			AttendancePosts:  []apiAttendancePost{},
		})
	case http.MethodPatch:
		var req apiStudentUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", "Request body must be valid JSON.")
			return
		}
		if _, ok, err := watchdog.AdminUserByLogin(login); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "student_load_failed", "Could not load watchdog state.")
			return
		} else if !ok {
			writeJSONError(w, http.StatusNotFound, "student_not_found", "Student not found.")
			return
		}

		if req.Status != nil {
			if normalized := watchdog.AdminUserStatusFromInput(*req.Status); normalized == "" {
				writeJSONError(w, http.StatusBadRequest, "invalid_status", "status must be one of student, apprentice, pisciner, staff or extern.")
				return
			}
		}
		if req.Status == nil && req.StatusOverridden == nil && req.IsBlacklisted == nil && req.BlacklistReason == nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_patch", "Provide status, status_overridden, is_blacklisted or blacklist_reason.")
			return
		}
		if _, err := watchdog.UpdateUserSettings(login, watchdog.UserSettingsPatch{
			IsBlacklisted:    req.IsBlacklisted,
			BlacklistReason:  req.BlacklistReason,
			Status:           req.Status,
			StatusOverridden: req.StatusOverridden,
		}); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "settings_update_failed", "Could not update student settings.")
			return
		}

		user, ok, err := watchdog.CurrentUserByLogin(login)
		if err == nil && ok {
			writeJSON(w, http.StatusOK, mapUserWithLiveDuration(user))
			return
		}

		summary, ok, err := watchdog.AdminUserByLogin(login)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "student_load_failed", "Could not load watchdog state.")
			return
		}
		if !ok {
			writeJSONError(w, http.StatusNotFound, "student_not_found", "Student not found.")
			return
		}
		writeJSON(w, http.StatusOK, mapAdminUser(summary))
	case http.MethodDelete:
		user, ok, err := watchdog.CurrentUserByLogin(login)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "student_load_failed", "Could not load watchdog state.")
			return
		}
		if !ok {
			writeJSONError(w, http.StatusNotFound, "student_not_found", "Student not found in watchdog state.")
			return
		}
		withPost := true
		if raw := strings.TrimSpace(r.URL.Query().Get("with_post")); raw != "" {
			parsed, err := strconv.ParseBool(raw)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_with_post", "with_post must be a boolean.")
				return
			}
			withPost = parsed
		}
		watchdog.DeleteStudent(login, withPost)
		writeJSON(w, http.StatusOK, apiMessageResponse{Message: "Deleted student " + user.Login42})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func studentMeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authUser := getAuthenticatedUser(r)
	if authUser == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	targetLogin := authUser.FtLogin
	if requestedLogin := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("login"))); requestedLogin != "" {
		if !isUserAllowedLoginOverride(authUser) {
			writeJSONError(w, http.StatusForbidden, "forbidden_login_override", "Only admins can simulate another student.")
			return
		}
		targetLogin = requestedLogin
	}

	if dayKey, historical, err := requestedDayKey(r); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_date", "date must use YYYY-MM-DD format.")
		return
	} else if historical {
		record, ok, err := watchdog.HistoricalStudentDayByLogin(targetLogin, dayKey)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "history_load_failed", "Could not load stored history.")
			return
		}
		if !ok {
			writeJSON(w, http.StatusOK, apiStudentMeResponse{
				Day:              dayKey,
				Live:             false,
				Login:            targetLogin,
				Tracked:          false,
				BadgeEvents:      []apiBadgeEvent{},
				LocationSessions: []apiLocationSession{},
				AttendancePosts:  []apiAttendancePost{},
			})
			return
		}
		writeJSON(w, http.StatusOK, mapHistoricalStudentResponse(record))
		return
	}

	user, ok, err := watchdog.CurrentUserByLogin(targetLogin)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "student_load_failed", "Could not load watchdog state.")
		return
	}
	badgeEvents := watchdog.SnapshotDailyBadgeEvents(targetLogin)
	locationSessions, locationsLoading := watchdog.SnapshotDailyLocationSessionsOrSchedule(targetLogin)
	if !ok {
		writeJSON(w, http.StatusOK, apiStudentMeResponse{
			Day:              todayDayKey(),
			Live:             true,
			Login:            targetLogin,
			Tracked:          false,
			LocationsLoading: locationsLoading,
			BadgeEvents:      mapBadgeEvents(badgeEvents),
			LocationSessions: mapLocationSessions(locationSessions),
			AttendancePosts:  []apiAttendancePost{},
		})
		return
	}

	retainedDuration := watchdog.CombinedRetainedDuration(
		badgeEvents,
		user.FirstAccess,
		user.LastAccess,
		locationSessions,
	)

	writeJSON(w, http.StatusOK, apiStudentMeResponse{
		Day:              todayDayKey(),
		Live:             true,
		Login:            targetLogin,
		Tracked:          true,
		LocationsLoading: locationsLoading,
		User:             mapUserWithDuration(user, retainedDuration),
		BadgeEvents:      mapBadgeEvents(badgeEvents),
		LocationSessions: mapLocationSessions(locationSessions),
		AttendancePosts:  []apiAttendancePost{},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func mapUsers(users []watchdog.User) []apiUserState {
	out := make([]apiUserState, 0, len(users))
	for _, user := range users {
		out = append(out, *mapUser(user))
	}
	return out
}

func mapUsersWithAttendancePosts(users []watchdog.User, postsByLogin map[string][]watchdog.AttendancePostRecord) []apiUserState {
	out := make([]apiUserState, 0, len(users))
	for _, user := range users {
		posts := postsByLogin[strings.ToLower(strings.TrimSpace(user.Login42))]
		watchdog.PopulateUserPostResult(&user, posts)
		item := mapUser(user)
		item.AttendancePosts = mapAttendancePosts(posts)
		out = append(out, *item)
	}
	return out
}

func mapUser(user watchdog.User) *apiUserState {
	return mapUserWithDuration(user, user.Duration)
}

func mapUserWithLiveDuration(user watchdog.User) *apiUserState {
	badgeEvents := watchdog.SnapshotDailyBadgeEvents(user.Login42)
	locationSessions := watchdog.SnapshotDailyLocationSessions(user.Login42)
	retainedDuration := watchdog.CombinedRetainedDuration(
		badgeEvents,
		user.FirstAccess,
		user.LastAccess,
		locationSessions,
	)
	return mapUserWithDuration(user, retainedDuration)
}

func mapUserWithDuration(user watchdog.User, retainedDuration time.Duration) *apiUserState {
	detectedStatus := user.Status42
	if detectedStatus == "" {
		detectedStatus = watchdog.AdminUserStatus(user.IsApprentice, user.Profile)
	}
	effectiveStatus := user.Status
	if effectiveStatus == "" {
		effectiveStatus = detectedStatus
	}
	return &apiUserState{
		ControlAccessID:   user.ControlAccessID,
		ControlAccessName: user.ControlAccessName,
		Login42:           user.Login42,
		ID42:              user.ID42,
		IsApprentice:      user.IsApprentice,
		Status:            effectiveStatus,
		PostResult:        user.PostResult,
		Status42:          detectedStatus,
		StatusOverridden:  user.StatusOverridden,
		IsBlacklisted:     user.IsBlacklisted,
		BadgePostingOff:   user.BadgePostingOff,
		BlacklistReason:   user.BlacklistReason,
		Profile:           profileToString(user.Profile),
		FirstAccess:       timePtr(user.FirstAccess),
		LastAccess:        timePtr(user.LastAccess),
		DurationSeconds:   int64(retainedDuration / time.Second),
		DurationHuman:     retainedDuration.String(),
		ErrorMessage:      errorMessageString(user.Error),
	}
}

func errorMessageString(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func mapBadgeEvents(events []watchdog.BadgeEvent) []apiBadgeEvent {
	if len(events) == 0 {
		return []apiBadgeEvent{}
	}

	out := make([]apiBadgeEvent, 0, len(events))
	for _, event := range events {
		out = append(out, apiBadgeEvent{
			Timestamp: event.Timestamp,
			DoorName:  event.DoorName,
		})
	}
	return out
}

func mapLocationSessions(sessions []watchdog.LocationSession) []apiLocationSession {
	if len(sessions) == 0 {
		return []apiLocationSession{}
	}

	out := make([]apiLocationSession, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, apiLocationSession{
			BeginAt: session.BeginAt,
			EndAt:   session.EndAt,
			Host:    session.Host,
			Counted: watchdog.IsCountedLocationSession(session),
			Ongoing: session.Ongoing,
		})
	}
	return out
}

func mapAttendancePosts(posts []watchdog.AttendancePostRecord) []apiAttendancePost {
	if len(posts) == 0 {
		return []apiAttendancePost{}
	}

	out := make([]apiAttendancePost, 0, len(posts))
	for _, post := range posts {
		out = append(out, apiAttendancePost{
			BeginAt:        post.BeginAt,
			EndAt:          post.EndAt,
			HTTPStatus:     post.HTTPStatus,
			ResponseStatus: post.ResponseStatus,
			ErrorMessage:   post.ErrorMessage,
			Success:        post.Success,
			CreatedAt:      post.CreatedAt,
			Payload:        post.Payload,
		})
	}
	return out
}

func mapHistoricalStudentResponse(record watchdog.HistoricalStudentDay) apiStudentMeResponse {
	return apiStudentMeResponse{
		Day:              record.DayKey,
		Live:             false,
		Login:            record.User.Login42,
		Tracked:          true,
		User:             mapUserWithDuration(record.User, record.RetainedDuration),
		BadgeEvents:      mapBadgeEvents(record.BadgeEvents),
		LocationSessions: mapLocationSessions(record.LocationSessions),
		AttendancePosts:  mapAttendancePosts(record.AttendancePosts),
	}
}

func mapAdminUsers(users []watchdog.AdminUserSummary) []apiAdminUserListItem {
	out := make([]apiAdminUserListItem, 0, len(users))
	for _, user := range users {
		out = append(out, mapAdminUser(user))
	}
	return out
}

func mapAdminUser(user watchdog.AdminUserSummary) apiAdminUserListItem {
	detectedStatus := user.Status42
	if detectedStatus == "" {
		detectedStatus = watchdog.AdminUserStatus(user.IsApprentice, user.Profile)
	}
	effectiveStatus := user.Status
	if effectiveStatus == "" {
		effectiveStatus = detectedStatus
	}
	return apiAdminUserListItem{
		Login42:                     user.Login42,
		PhotoURL:                    watchdog.CachedUserPhotoURLOrSchedule(user.Login42),
		Status:                      effectiveStatus,
		Status42:                    detectedStatus,
		StatusOverridden:            user.StatusOverridden,
		IsBlacklisted:               user.IsBlacklisted,
		BadgePostingOff:             user.BadgePostingOff,
		BlacklistReason:             user.BlacklistReason,
		LastBadgeAt:                 timePtr(user.LastBadgeAt),
		LastBadgeDayDurationSeconds: int64(user.DayDuration / time.Second),
		LastBadgeDayDurationHuman:   user.DayDuration.String(),
	}
}

func mapAdminUserDetail(user watchdog.AdminUserSummary, days []watchdog.StudentAttendanceDaySummary) apiAdminUserDetailResponse {
	payload := apiAdminUserDetailResponse{
		apiAdminUserListItem: mapAdminUser(user),
		Days:                 make([]apiAdminStudentDay, 0, len(days)),
	}
	for _, day := range days {
		payload.Days = append(payload.Days, apiAdminStudentDay{
			Day:             day.DayKey,
			Live:            day.Live,
			FirstAccess:     timePtr(day.FirstAccess),
			LastAccess:      timePtr(day.LastAccess),
			DurationSeconds: int64(day.Duration / time.Second),
			DurationHuman:   day.Duration.String(),
			Status:          day.Status,
			Loading:         day.Loading,
		})
	}
	return payload
}

func adminUserStatusLabel(user watchdog.AdminUserSummary) string {
	if user.Status != "" {
		return user.Status
	}
	return watchdog.AdminUserStatus(user.IsApprentice, user.Profile)
}

func requestedDayKey(r *http.Request) (string, bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("date"))
	if raw == "" {
		return todayDayKey(), false, nil
	}

	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		loc = time.Local
	}
	parsed, err := time.ParseInLocation("2006-01-02", raw, loc)
	if err != nil {
		return "", false, err
	}
	dayKey := parsed.Format("2006-01-02")
	return dayKey, dayKey != todayDayKey(), nil
}

func requestedMonthKey(r *http.Request) (string, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("month"))
	if raw == "" {
		return todayDayKey()[:7], nil
	}

	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		loc = time.Local
	}
	parsed, err := time.ParseInLocation("2006-01", raw, loc)
	if err != nil {
		return "", err
	}
	return parsed.Format("2006-01"), nil
}

func todayDayKey() string {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		loc = time.Local
	}
	return time.Now().In(loc).Format("2006-01-02")
}

func timePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func profileToString(profile watchdog.ProfileType) string {
	switch profile {
	case watchdog.Staff:
		return "staff"
	case watchdog.Pisciner:
		return "pisciner"
	case watchdog.Student:
		return "student"
	default:
		return "extern"
	}
}
