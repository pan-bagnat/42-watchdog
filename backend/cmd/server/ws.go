package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
	"watchdog/watchdog"

	"github.com/gorilla/websocket"
)

type wsMessage struct {
	Type                        string                      `json:"type"`
	Login                       string                      `json:"login"`
	Day                         string                      `json:"day,omitempty"`
	Month                       string                      `json:"month,omitempty"`
	Timestamp                   time.Time                   `json:"timestamp,omitempty"`
	DoorName                    string                      `json:"door_name,omitempty"`
	BadgeDelaySeconds           int64                       `json:"badge_delay_seconds,omitempty"`
	DayPayload                  *apiStudentMeResponse       `json:"day_payload,omitempty"`
	DaySummary                  *apiAdminStudentDay         `json:"day_summary,omitempty"`
	MonthPayload                *apiAdminUserDetailResponse `json:"month_payload,omitempty"`
	LastBadgeAt                 *time.Time                  `json:"last_badge_at,omitempty"`
	LastBadgeDayDurationSeconds int64                       `json:"last_badge_day_duration_seconds,omitempty"`
	LastBadgeDayDurationHuman   string                      `json:"last_badge_day_duration_human,omitempty"`
}

type wsHub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

func newWSHub() *wsHub {
	return &wsHub{
		clients: make(map[*websocket.Conn]struct{}),
	}
}

func (hub *wsHub) add(conn *websocket.Conn) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	hub.clients[conn] = struct{}{}
}

func (hub *wsHub) remove(conn *websocket.Conn) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	delete(hub.clients, conn)
}

func (hub *wsHub) broadcast(payload wsMessage) {
	message, err := json.Marshal(payload)
	if err != nil {
		return
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()

	for conn := range hub.clients {
		_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
			_ = conn.Close()
			delete(hub.clients, conn)
		}
	}
}

var liveUpdatesHub = newWSHub()

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return true
		}
		return true
	},
}

func initLiveUpdates() {
	watchdog.RegisterBadgeUpdateListener(func(event watchdog.BadgeUpdateEvent) {
		message := wsMessage{
			Type:              "badge_received",
			Login:             event.Login,
			Day:               todayDayKey(),
			Timestamp:         event.Timestamp,
			DoorName:          event.DoorName,
			BadgeDelaySeconds: event.BadgeDelaySeconds,
		}
		enrichLiveUpdateMessage(&message, event.Login, todayDayKey())
		liveUpdatesHub.broadcast(message)
	})
	watchdog.RegisterLocationSessionsUpdateListener(func(event watchdog.LocationSessionsUpdateEvent) {
		message := wsMessage{
			Type:  "location_sessions_updated",
			Login: event.Login,
			Day:   event.DayKey,
		}
		enrichLiveUpdateMessage(&message, event.Login, event.DayKey)
		liveUpdatesHub.broadcast(message)
	})
	watchdog.RegisterLocationMonthUpdateListener(func(event watchdog.LocationMonthUpdateEvent) {
		message := wsMessage{
			Type:  "month_updated",
			Login: event.Login,
			Month: event.MonthKey,
		}
		enrichLiveUpdateMonthMessage(&message, event.Login, event.MonthKey)
		liveUpdatesHub.broadcast(message)
	})
}

func enrichLiveUpdateMessage(message *wsMessage, login, dayKey string) {
	if message == nil {
		return
	}

	trimmedLogin := strings.ToLower(strings.TrimSpace(login))
	trimmedDayKey := strings.TrimSpace(dayKey)
	if trimmedLogin == "" || trimmedDayKey == "" {
		return
	}

	message.Login = trimmedLogin
	message.Day = trimmedDayKey

	if summary, ok := buildLiveUpdateDaySummary(trimmedLogin, trimmedDayKey); ok {
		message.DaySummary = &summary
		message.LastBadgeAt = summary.LastAccess
		message.LastBadgeDayDurationSeconds = summary.DurationSeconds
		message.LastBadgeDayDurationHuman = summary.DurationHuman
	}

	if payload, ok := buildLiveUpdateDayPayload(trimmedLogin, trimmedDayKey); ok {
		message.DayPayload = &payload
	}
}

func enrichLiveUpdateMonthMessage(message *wsMessage, login, monthKey string) {
	if message == nil {
		return
	}

	trimmedLogin := strings.ToLower(strings.TrimSpace(login))
	trimmedMonthKey := strings.TrimSpace(monthKey)
	if trimmedLogin == "" || trimmedMonthKey == "" {
		return
	}

	message.Login = trimmedLogin
	message.Month = trimmedMonthKey

	if payload, ok := buildLiveUpdateMonthPayload(trimmedLogin, trimmedMonthKey); ok {
		message.MonthPayload = &payload
		message.LastBadgeAt = payload.LastBadgeAt
		message.LastBadgeDayDurationSeconds = payload.LastBadgeDayDurationSeconds
		message.LastBadgeDayDurationHuman = payload.LastBadgeDayDurationHuman
	}
}

func buildLiveUpdateDaySummary(login, dayKey string) (apiAdminStudentDay, bool) {
	var (
		dayType                 string
		dayTypeLabel            string
		requiredAttendanceHours *float64
	)
	if monthDays, err := watchdog.AttendanceDaysForLogin(login, dayKey[:7]); err == nil {
		for _, day := range monthDays {
			if day.DayKey != dayKey {
				continue
			}
			dayType = day.DayType
			dayTypeLabel = day.DayTypeLabel
			requiredAttendanceHours = day.RequiredAttendanceHours
			break
		}
	}

	if dayKey == todayDayKey() {
		badgeEvents, badgeLoading := watchdog.SnapshotDailyEffectiveBadgeEventsOrSchedule(login)
		locationSessions, locationsLoading := watchdog.SnapshotDailyLocationSessionsOrSchedule(login)
		_ = badgeLoading
		_ = locationsLoading

		user, ok, err := watchdog.CurrentUserByLogin(login)
		if err != nil {
			return apiAdminStudentDay{}, false
		}
		if !ok {
			fallbackUser, tracked, err := liveFallbackUserByLogin(login, badgeEvents, locationSessions)
			if err != nil || !tracked {
				return apiAdminStudentDay{}, false
			}
			user = fallbackUser
		}
		if user.FirstAccess.IsZero() && len(badgeEvents) > 0 {
			user.FirstAccess = badgeEvents[0].Timestamp
		}
		if user.LastAccess.IsZero() && len(badgeEvents) > 0 {
			user.LastAccess = badgeEvents[len(badgeEvents)-1].Timestamp
		}
		duration := watchdog.CombinedRetainedDuration(badgeEvents, user.FirstAccess, user.LastAccess, locationSessions)
		return apiAdminStudentDay{
			Day:                     dayKey,
			Live:                    true,
			FirstAccess:             timePtr(user.FirstAccess),
			LastAccess:              timePtr(user.LastAccess),
			DurationSeconds:         int64(duration / time.Second),
			DurationHuman:           duration.String(),
			Status:                  user.Status,
			DayType:                 dayType,
			DayTypeLabel:            dayTypeLabel,
			RequiredAttendanceHours: requiredAttendanceHours,
		}, true
	}

	record, ok, err := watchdog.HistoricalStudentDayByLogin(login, dayKey)
	if err != nil || !ok {
		return apiAdminStudentDay{}, false
	}
	return apiAdminStudentDay{
		Day:                     dayKey,
		Live:                    false,
		FirstAccess:             timePtr(record.User.FirstAccess),
		LastAccess:              timePtr(record.User.LastAccess),
		DurationSeconds:         int64(record.RetainedDuration / time.Second),
		DurationHuman:           record.RetainedDuration.String(),
		Status:                  record.User.Status,
		DayType:                 dayType,
		DayTypeLabel:            dayTypeLabel,
		RequiredAttendanceHours: requiredAttendanceHours,
	}, true
}

func buildLiveUpdateDayPayload(login, dayKey string) (apiStudentMeResponse, bool) {
	if dayKey == todayDayKey() {
		user, ok, err := watchdog.CurrentUserByLogin(login)
		if err != nil {
			return apiStudentMeResponse{}, false
		}
		badgeEvents, badgeLoading := watchdog.SnapshotDailyEffectiveBadgeEventsOrSchedule(login)
		locationSessions, locationsLoading := watchdog.SnapshotDailyLocationSessionsOrSchedule(login)
		locationsLoading = locationsLoading || badgeLoading
		if !ok {
			fallbackUser, tracked, err := liveFallbackUserByLogin(login, badgeEvents, locationSessions)
			if err != nil || !tracked {
				return apiStudentMeResponse{}, false
			}
			return apiStudentMeResponse{
				Day:              dayKey,
				Live:             true,
				Login:            login,
				Tracked:          tracked,
				LocationsLoading: locationsLoading,
				User:             mapUserPtrWithDuration(fallbackUser, fallbackUser.Duration, tracked),
				BadgeEvents:      mapBadgeEvents(badgeEvents),
				LocationSessions: mapLocationSessions(locationSessions),
				AttendancePosts:  []apiAttendancePost{},
			}, true
		}
		if user.FirstAccess.IsZero() && len(badgeEvents) > 0 {
			user.FirstAccess = badgeEvents[0].Timestamp
		}
		if user.LastAccess.IsZero() && len(badgeEvents) > 0 {
			user.LastAccess = badgeEvents[len(badgeEvents)-1].Timestamp
		}
		retainedDuration := watchdog.CombinedRetainedDuration(
			badgeEvents,
			user.FirstAccess,
			user.LastAccess,
			locationSessions,
		)
		return apiStudentMeResponse{
			Day:              dayKey,
			Live:             true,
			Login:            login,
			Tracked:          true,
			LocationsLoading: locationsLoading,
			User:             mapUserWithDuration(user, retainedDuration),
			BadgeEvents:      mapBadgeEvents(badgeEvents),
			LocationSessions: mapLocationSessions(locationSessions),
			AttendancePosts:  []apiAttendancePost{},
		}, true
	}

	record, ok, err := watchdog.HistoricalStudentDayByLogin(login, dayKey)
	if err != nil || !ok {
		return apiStudentMeResponse{}, false
	}
	return mapHistoricalStudentResponse(record), true
}

func buildLiveUpdateMonthPayload(login, monthKey string) (apiAdminUserDetailResponse, bool) {
	days, err := watchdog.AttendanceDaysForLogin(login, monthKey)
	if err != nil {
		return apiAdminUserDetailResponse{}, false
	}

	payload := apiAdminUserDetailResponse{
		apiAdminUserListItem: apiAdminUserListItem{
			Login42:          login,
			PhotoURL:         watchdog.CachedUserPhotoURLOrSchedule(login),
			Status:           "student",
			Status42:         "student",
			StatusOverridden: false,
			IsBlacklisted:    false,
			BadgePostingOff:  false,
		},
		Days: make([]apiAdminStudentDay, 0, len(days)),
	}

	if user, ok, err := watchdog.AdminUserByLogin(login); err == nil && ok {
		payload.apiAdminUserListItem = mapAdminUser(user)
	} else if err == nil {
		if settings, settingsOK, settingsErr := watchdog.UserSettingsByLogin(login); settingsErr == nil && settingsOK {
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
	} else {
		return apiAdminUserDetailResponse{}, false
	}

	for _, day := range days {
		payload.Days = append(payload.Days, apiAdminStudentDay{
			Day:                     day.DayKey,
			Live:                    day.Live,
			FirstAccess:             timePtr(day.FirstAccess),
			LastAccess:              timePtr(day.LastAccess),
			DurationSeconds:         int64(day.Duration / time.Second),
			DurationHuman:           day.Duration.String(),
			Status:                  day.Status,
			Loading:                 day.Loading,
			DayType:                 day.DayType,
			DayTypeLabel:            day.DayTypeLabel,
			RequiredAttendanceHours: day.RequiredAttendanceHours,
		})
	}

	return payload, true
}

func liveUpdatesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	liveUpdatesHub.add(conn)
	defer func() {
		liveUpdatesHub.remove(conn)
		_ = conn.Close()
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
