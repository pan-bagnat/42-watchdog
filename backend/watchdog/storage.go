package watchdog

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"watchdog/config"

	_ "github.com/lib/pq"
)

type AttendancePostRecord struct {
	DayKey          string        `json:"day_key"`
	Login42         string        `json:"login_42"`
	ID42            string        `json:"id_42"`
	ControlAccessID int           `json:"control_access_id"`
	BeginAt         *time.Time    `json:"begin_at,omitempty"`
	EndAt           *time.Time    `json:"end_at,omitempty"`
	Payload         APIAttendance `json:"payload"`
	HTTPStatus      *int          `json:"http_status,omitempty"`
	ResponseStatus  string        `json:"response_status,omitempty"`
	ErrorMessage    string        `json:"error_message,omitempty"`
	Success         bool          `json:"success"`
	CreatedAt       time.Time     `json:"created_at"`
}

type HistoricalStudentDay struct {
	DayKey           string                 `json:"day_key"`
	User             User                   `json:"user"`
	BadgeEvents      []BadgeEvent           `json:"badge_events"`
	LocationSessions []LocationSession      `json:"location_sessions"`
	AttendancePosts  []AttendancePostRecord `json:"attendance_posts"`
	RetainedDuration time.Duration          `json:"retained_duration"`
	BadgeDuration    time.Duration          `json:"badge_duration"`
}

type DayAvailability struct {
	DayKey       string `json:"day_key"`
	StudentCount int    `json:"student_count"`
	Live         bool   `json:"live"`
}

type StudentAttendanceDaySummary struct {
	DayKey      string        `json:"day_key"`
	FirstAccess time.Time     `json:"first_access"`
	LastAccess  time.Time     `json:"last_access"`
	Duration    time.Duration `json:"duration"`
	Status      string        `json:"status"`
	Live        bool          `json:"live"`
	Loading     bool          `json:"loading"`
}

type AdminUserSummary struct {
	Login42      string      `json:"login_42"`
	IsApprentice bool        `json:"is_apprentice"`
	Profile      ProfileType `json:"profile"`
	LastBadgeAt  time.Time   `json:"last_badge_at"`
}

var (
	storageDB                      *sql.DB
	storageRuntimeDay              string
	storageDayMutex                sync.Mutex
	historicalMonthFetchesMu       sync.Mutex
	historicalMonthFetchesInFlight = map[string]struct{}{}
)

const storageSchema = `
CREATE TABLE IF NOT EXISTS watchdog_current_users (
	day_key TEXT NOT NULL,
	control_access_id INTEGER NOT NULL,
	control_access_name TEXT NOT NULL,
	login_42 TEXT NOT NULL,
	id_42 TEXT NOT NULL,
	is_apprentice INTEGER NOT NULL,
	profile INTEGER NOT NULL,
	first_access TEXT,
	last_access TEXT,
	duration_seconds BIGINT NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT '',
	error_message TEXT,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (day_key, control_access_id)
);

CREATE INDEX IF NOT EXISTS idx_watchdog_current_users_login
	ON watchdog_current_users(day_key, login_42);

CREATE TABLE IF NOT EXISTS watchdog_day_profiles (
	day_key TEXT NOT NULL,
	login_42 TEXT NOT NULL,
	control_access_id INTEGER NOT NULL,
	control_access_name TEXT NOT NULL,
	id_42 TEXT NOT NULL,
	is_apprentice INTEGER NOT NULL,
	profile INTEGER NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (day_key, login_42)
);

CREATE TABLE IF NOT EXISTS watchdog_badge_events (
	id BIGSERIAL PRIMARY KEY,
	day_key TEXT NOT NULL,
	login_42 TEXT NOT NULL,
	occurred_at TEXT NOT NULL,
	door_name TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_watchdog_badge_events_login
	ON watchdog_badge_events(day_key, login_42, occurred_at);

CREATE TABLE IF NOT EXISTS watchdog_daily_student_summaries (
	day_key TEXT NOT NULL,
	login_42 TEXT NOT NULL,
	control_access_id INTEGER NOT NULL,
	control_access_name TEXT NOT NULL,
	id_42 TEXT NOT NULL,
	is_apprentice INTEGER NOT NULL,
	profile INTEGER NOT NULL,
	first_access TEXT,
	last_access TEXT,
	badge_duration_seconds BIGINT NOT NULL DEFAULT 0,
	retained_duration_seconds BIGINT NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT '',
	error_message TEXT,
	finalized_at TEXT NOT NULL,
	PRIMARY KEY (day_key, login_42)
);

CREATE TABLE IF NOT EXISTS watchdog_daily_location_sessions (
	id BIGSERIAL PRIMARY KEY,
	day_key TEXT NOT NULL,
	login_42 TEXT NOT NULL,
	begin_at TEXT NOT NULL,
	end_at TEXT NOT NULL,
	host TEXT NOT NULL DEFAULT '',
	ongoing INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_watchdog_daily_location_sessions_login
	ON watchdog_daily_location_sessions(day_key, login_42, begin_at);

CREATE TABLE IF NOT EXISTS watchdog_historical_location_fetches (
	day_key TEXT NOT NULL,
	login_42 TEXT NOT NULL,
	fetched_at TEXT NOT NULL,
	PRIMARY KEY (day_key, login_42)
);

CREATE TABLE IF NOT EXISTS watchdog_attendance_posts (
	id BIGSERIAL PRIMARY KEY,
	day_key TEXT NOT NULL,
	login_42 TEXT NOT NULL,
	id_42 TEXT NOT NULL,
	control_access_id INTEGER NOT NULL,
	begin_at TEXT,
	end_at TEXT,
	payload_json TEXT NOT NULL,
	http_status INTEGER,
	response_status TEXT,
	error_message TEXT,
	success INTEGER NOT NULL,
	created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_watchdog_attendance_posts_login
	ON watchdog_attendance_posts(day_key, login_42, created_at);

CREATE TABLE IF NOT EXISTS watchdog_finalized_days (
	day_key TEXT PRIMARY KEY,
	finalized_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS watchdog_profile_photos (
	login_42 TEXT PRIMARY KEY,
	photo_url TEXT NOT NULL DEFAULT '',
	fetched_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
`

func InitStorage() error {
	databaseURL := strings.TrimSpace(config.ConfigData.Storage.DatabaseURL)
	if databaseURL == "" {
		databaseURL = "postgres://watchdog:watchdog@watchdog-db:5432/watchdog?sslmode=disable"
	}

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("open postgres db: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return fmt.Errorf("ping postgres db: %w", err)
	}

	storageDB = db
	if _, err := storageExec(storageSchema); err != nil {
		_ = db.Close()
		storageDB = nil
		return fmt.Errorf("init postgres schema: %w", err)
	}

	Log("[WATCHDOG] 💽 PostgreSQL storage ready")
	return restorePersistentState()
}

func CloseStorage() {
	if storageDB != nil {
		_ = storageDB.Close()
	}
}

func storageExec(query string, args ...any) (sql.Result, error) {
	return storageDB.Exec(rebindPostgres(query), args...)
}

func storageQuery(query string, args ...any) (*sql.Rows, error) {
	return storageDB.Query(rebindPostgres(query), args...)
}

func storageQueryRow(query string, args ...any) *sql.Row {
	return storageDB.QueryRow(rebindPostgres(query), args...)
}

func rebindPostgres(query string) string {
	if !strings.Contains(query, "?") {
		return query
	}

	var out strings.Builder
	out.Grow(len(query) + 16)
	index := 1
	for _, char := range query {
		if char == '?' {
			out.WriteString(fmt.Sprintf("$%d", index))
			index++
			continue
		}
		out.WriteRune(char)
	}
	return out.String()
}

func restorePersistentState() error {
	storageDayMutex.Lock()
	defer storageDayMutex.Unlock()

	todayKey := todayDayKeyInParis()
	staleDayKey, err := loadCurrentUsersDayKey()
	if err != nil {
		return err
	}

	if staleDayKey != "" && staleDayKey != todayKey {
		finalized, err := isDayFinalized(staleDayKey)
		if err != nil {
			return err
		}
		if !finalized {
			Log(fmt.Sprintf("[WATCHDOG] 🗄️  Finalizing stale day %s from persistent storage", staleDayKey))
			if err := finalizeDayWithOverrides(staleDayKey, nil); err != nil {
				return err
			}
		}
		if err := deleteCurrentUsersForDay(staleDayKey); err != nil {
			return err
		}
	}

	dailyLocationSessionsMutex.Lock()
	dailyLocationSessionsDayKey = todayKey
	dailyLocationSessions = make(map[string]locationSessionsCacheEntry)
	dailyLocationSessionsMutex.Unlock()

	storageRuntimeDay = todayKey
	Log(fmt.Sprintf("[WATCHDOG] 🗃️  Runtime state ready for %s", todayKey))
	return nil
}

func ensureRuntimeDayState() {
	storageDayMutex.Lock()
	defer storageDayMutex.Unlock()

	todayKey := todayDayKeyInParis()
	if storageRuntimeDay == "" {
		storageRuntimeDay = todayKey
		return
	}
	if storageRuntimeDay == todayKey {
		return
	}

	previousDay := storageRuntimeDay
	finalized, err := isDayFinalized(previousDay)
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not check finalized day %s: %v", previousDay, err))
	} else if !finalized {
		Log(fmt.Sprintf("[WATCHDOG] 🗄️  Finalizing previous day %s after day rollover", previousDay))
		if err := finalizeDayWithOverrides(previousDay, nil); err != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not finalize day %s: %v", previousDay, err))
		}
	}

	if err := deleteCurrentUsersForDay(previousDay); err != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not clear current day users for %s: %v", previousDay, err))
	}

	dailyLocationSessionsMutex.Lock()
	dailyLocationSessionsDayKey = todayKey
	dailyLocationSessions = make(map[string]locationSessionsCacheEntry)
	dailyLocationSessionsMutex.Unlock()

	storageRuntimeDay = todayKey
}

func currentRuntimeDayKey() string {
	storageDayMutex.Lock()
	defer storageDayMutex.Unlock()
	if storageRuntimeDay == "" {
		storageRuntimeDay = todayDayKeyInParis()
	}
	return storageRuntimeDay
}

func saveDayProfile(dayKey string, user User) error {
	if storageDB == nil {
		return nil
	}

	_, err := storageExec(`
		INSERT INTO watchdog_day_profiles (
			day_key, login_42, control_access_id, control_access_name, id_42, is_apprentice, profile, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(day_key, login_42) DO UPDATE SET
			control_access_id = excluded.control_access_id,
			control_access_name = excluded.control_access_name,
			id_42 = excluded.id_42,
			is_apprentice = excluded.is_apprentice,
			profile = excluded.profile,
			updated_at = excluded.updated_at
	`,
		dayKey,
		strings.ToLower(strings.TrimSpace(user.Login42)),
		user.ControlAccessID,
		user.ControlAccessName,
		user.ID42,
		boolToInt(user.IsApprentice),
		int(user.Profile),
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func saveCurrentUser(dayKey string, user User) error {
	if storageDB == nil {
		return nil
	}

	var firstAccess any
	if !user.FirstAccess.IsZero() {
		firstAccess = user.FirstAccess.UTC().Format(time.RFC3339Nano)
	}
	var lastAccess any
	if !user.LastAccess.IsZero() {
		lastAccess = user.LastAccess.UTC().Format(time.RFC3339Nano)
	}
	var errorMessage any
	if user.Error != nil {
		errorMessage = user.Error.Error()
	}

	_, err := storageExec(`
		INSERT INTO watchdog_current_users (
			day_key, control_access_id, control_access_name, login_42, id_42, is_apprentice, profile,
			first_access, last_access, duration_seconds, status, error_message, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(day_key, control_access_id) DO UPDATE SET
			control_access_name = excluded.control_access_name,
			login_42 = excluded.login_42,
			id_42 = excluded.id_42,
			is_apprentice = excluded.is_apprentice,
			profile = excluded.profile,
			first_access = excluded.first_access,
			last_access = excluded.last_access,
			duration_seconds = excluded.duration_seconds,
			status = excluded.status,
			error_message = excluded.error_message,
			updated_at = excluded.updated_at
	`,
		dayKey,
		user.ControlAccessID,
		user.ControlAccessName,
		strings.ToLower(strings.TrimSpace(user.Login42)),
		user.ID42,
		boolToInt(user.IsApprentice),
		int(user.Profile),
		firstAccess,
		lastAccess,
		int64(user.Duration/time.Second),
		user.Status,
		errorMessage,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func deleteCurrentUser(dayKey string, controlAccessID int) error {
	if storageDB == nil {
		return nil
	}
	_, err := storageExec(`DELETE FROM watchdog_current_users WHERE day_key = ? AND control_access_id = ?`, dayKey, controlAccessID)
	return err
}

func deleteCurrentUsersForDay(dayKey string) error {
	if storageDB == nil || strings.TrimSpace(dayKey) == "" {
		return nil
	}
	_, err := storageExec(`DELETE FROM watchdog_current_users WHERE day_key = ?`, dayKey)
	return err
}

func deleteCurrentDayDataForLogin(dayKey, login string) error {
	if storageDB == nil {
		return nil
	}
	login = strings.ToLower(strings.TrimSpace(login))
	_, err := storageExec(`DELETE FROM watchdog_day_profiles WHERE day_key = ? AND login_42 = ?`, dayKey, login)
	if err != nil {
		return err
	}
	_, err = storageExec(`DELETE FROM watchdog_badge_events WHERE day_key = ? AND login_42 = ?`, dayKey, login)
	return err
}

func saveBadgeEvent(dayKey, login string, ts time.Time, doorName string) error {
	if storageDB == nil {
		return nil
	}
	login = strings.ToLower(strings.TrimSpace(login))
	_, err := storageExec(`
		INSERT INTO watchdog_badge_events (day_key, login_42, occurred_at, door_name, created_at)
		VALUES (?, ?, ?, ?, ?)
	`,
		dayKey,
		login,
		ts.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(doorName),
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func recordAttendancePost(dayKey string, user User, payload APIAttendance, httpStatus *int, responseStatus, errorMessage string, success bool) error {
	if storageDB == nil {
		return nil
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	var beginAt any
	if strings.TrimSpace(payload.Begin_at) != "" {
		beginAt = payload.Begin_at
	}
	var endAt any
	if strings.TrimSpace(payload.End_at) != "" {
		endAt = payload.End_at
	}
	var statusValue any
	if httpStatus != nil {
		statusValue = *httpStatus
	}
	var responseValue any
	if strings.TrimSpace(responseStatus) != "" {
		responseValue = responseStatus
	}
	var errorValue any
	if strings.TrimSpace(errorMessage) != "" {
		errorValue = errorMessage
	}

	_, err = storageExec(`
		INSERT INTO watchdog_attendance_posts (
			day_key, login_42, id_42, control_access_id, begin_at, end_at, payload_json,
			http_status, response_status, error_message, success, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		dayKey,
		strings.ToLower(strings.TrimSpace(user.Login42)),
		user.ID42,
		user.ControlAccessID,
		beginAt,
		endAt,
		string(payloadBytes),
		statusValue,
		responseValue,
		errorValue,
		boolToInt(success),
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func loadCurrentUsersDayKey() (string, error) {
	if storageDB == nil {
		return "", nil
	}
	var dayKey sql.NullString
	err := storageQueryRow(`
		SELECT day_key
		FROM (
			SELECT day_key FROM watchdog_current_users
			UNION ALL
			SELECT day_key FROM watchdog_day_profiles
		) AS all_days
		ORDER BY day_key DESC
		LIMIT 1
	`).Scan(&dayKey)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(dayKey.String), nil
}

func loadCurrentUsersForDay(dayKey string) ([]User, error) {
	if storageDB == nil || strings.TrimSpace(dayKey) == "" {
		return nil, nil
	}

	rows, err := storageQuery(`
		SELECT control_access_id, control_access_name, login_42, id_42, is_apprentice, profile,
			first_access, last_access, duration_seconds, status, error_message
		FROM watchdog_current_users
		WHERE day_key = ?
	`, dayKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		var isApprentice int
		var profile int
		var firstAccess sql.NullString
		var lastAccess sql.NullString
		var durationSeconds int64
		var status sql.NullString
		var errorMessage sql.NullString

		if err := rows.Scan(
			&user.ControlAccessID,
			&user.ControlAccessName,
			&user.Login42,
			&user.ID42,
			&isApprentice,
			&profile,
			&firstAccess,
			&lastAccess,
			&durationSeconds,
			&status,
			&errorMessage,
		); err != nil {
			return nil, err
		}

		user.IsApprentice = isApprentice == 1
		user.Profile = ProfileType(profile)
		user.Duration = time.Duration(durationSeconds) * time.Second
		user.Status = status.String
		if firstAccess.Valid {
			user.FirstAccess, err = time.Parse(time.RFC3339Nano, firstAccess.String)
			if err != nil {
				return nil, err
			}
		}
		if lastAccess.Valid {
			user.LastAccess, err = time.Parse(time.RFC3339Nano, lastAccess.String)
			if err != nil {
				return nil, err
			}
		}
		if errorMessage.Valid && strings.TrimSpace(errorMessage.String) != "" {
			user.Error = fmt.Errorf("%s", errorMessage.String)
		}

		users = append(users, user)
	}
	return users, rows.Err()
}

func loadCurrentUserByLogin(dayKey, login string) (User, bool, error) {
	if storageDB == nil || strings.TrimSpace(dayKey) == "" {
		return User{}, false, nil
	}

	login = normalizeLogin(login)
	var (
		user            User
		isApprentice    int
		profile         int
		firstAccess     sql.NullString
		lastAccess      sql.NullString
		durationSeconds int64
		status          sql.NullString
		errorMessage    sql.NullString
	)

	err := storageQueryRow(`
		SELECT control_access_id, control_access_name, login_42, id_42, is_apprentice, profile,
			first_access, last_access, duration_seconds, status, error_message
		FROM watchdog_current_users
		WHERE day_key = ? AND login_42 = ?
	`, dayKey, login).Scan(
		&user.ControlAccessID,
		&user.ControlAccessName,
		&user.Login42,
		&user.ID42,
		&isApprentice,
		&profile,
		&firstAccess,
		&lastAccess,
		&durationSeconds,
		&status,
		&errorMessage,
	)
	if err == sql.ErrNoRows {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}

	user.IsApprentice = isApprentice == 1
	user.Profile = ProfileType(profile)
	user.Duration = time.Duration(durationSeconds) * time.Second
	user.Status = status.String
	if firstAccess.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, firstAccess.String)
		if err != nil {
			return User{}, false, err
		}
		user.FirstAccess = parsed
	}
	if lastAccess.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, lastAccess.String)
		if err != nil {
			return User{}, false, err
		}
		user.LastAccess = parsed
	}
	if errorMessage.Valid && strings.TrimSpace(errorMessage.String) != "" {
		user.Error = fmt.Errorf("%s", errorMessage.String)
	}

	return user, true, nil
}

func loadCurrentUserByControlAccessID(dayKey string, controlAccessID int) (User, bool, error) {
	if storageDB == nil || strings.TrimSpace(dayKey) == "" {
		return User{}, false, nil
	}

	var (
		user            User
		isApprentice    int
		profile         int
		firstAccess     sql.NullString
		lastAccess      sql.NullString
		durationSeconds int64
		status          sql.NullString
		errorMessage    sql.NullString
	)

	err := storageQueryRow(`
		SELECT control_access_id, control_access_name, login_42, id_42, is_apprentice, profile,
			first_access, last_access, duration_seconds, status, error_message
		FROM watchdog_current_users
		WHERE day_key = ? AND control_access_id = ?
	`, dayKey, controlAccessID).Scan(
		&user.ControlAccessID,
		&user.ControlAccessName,
		&user.Login42,
		&user.ID42,
		&isApprentice,
		&profile,
		&firstAccess,
		&lastAccess,
		&durationSeconds,
		&status,
		&errorMessage,
	)
	if err == sql.ErrNoRows {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}

	user.IsApprentice = isApprentice == 1
	user.Profile = ProfileType(profile)
	user.Duration = time.Duration(durationSeconds) * time.Second
	user.Status = status.String
	if firstAccess.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, firstAccess.String)
		if err != nil {
			return User{}, false, err
		}
		user.FirstAccess = parsed
	}
	if lastAccess.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, lastAccess.String)
		if err != nil {
			return User{}, false, err
		}
		user.LastAccess = parsed
	}
	if errorMessage.Valid && strings.TrimSpace(errorMessage.String) != "" {
		user.Error = fmt.Errorf("%s", errorMessage.String)
	}

	return user, true, nil
}

func loadDayProfiles(dayKey string) (map[string]User, error) {
	if storageDB == nil || strings.TrimSpace(dayKey) == "" {
		return map[string]User{}, nil
	}

	rows, err := storageQuery(`
		SELECT login_42, control_access_id, control_access_name, id_42, is_apprentice, profile
		FROM watchdog_day_profiles
		WHERE day_key = ?
	`, dayKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	profiles := make(map[string]User)
	for rows.Next() {
		var login string
		var user User
		var isApprentice int
		var profile int
		if err := rows.Scan(
			&login,
			&user.ControlAccessID,
			&user.ControlAccessName,
			&user.ID42,
			&isApprentice,
			&profile,
		); err != nil {
			return nil, err
		}
		user.Login42 = login
		user.IsApprentice = isApprentice == 1
		user.Profile = ProfileType(profile)
		profiles[login] = user
	}
	return profiles, rows.Err()
}

func loadBadgeEventsByLogin(dayKey string) (map[string][]BadgeEvent, error) {
	if storageDB == nil || strings.TrimSpace(dayKey) == "" {
		return map[string][]BadgeEvent{}, nil
	}

	rows, err := storageQuery(`
		SELECT login_42, occurred_at, door_name
		FROM watchdog_badge_events
		WHERE day_key = ?
		ORDER BY login_42, occurred_at
	`, dayKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make(map[string][]BadgeEvent)
	for rows.Next() {
		var login string
		var occurredAt string
		var doorName string
		if err := rows.Scan(&login, &occurredAt, &doorName); err != nil {
			return nil, err
		}
		ts, err := time.Parse(time.RFC3339Nano, occurredAt)
		if err != nil {
			return nil, err
		}
		login = strings.ToLower(strings.TrimSpace(login))
		events[login] = append(events[login], BadgeEvent{
			Timestamp: ts,
			DoorName:  doorName,
		})
	}
	return events, rows.Err()
}

func loadBadgeEventsForLogin(dayKey, login string) ([]BadgeEvent, error) {
	if storageDB == nil || strings.TrimSpace(dayKey) == "" {
		return nil, nil
	}

	login = normalizeLogin(login)
	rows, err := storageQuery(`
		SELECT occurred_at, door_name
		FROM watchdog_badge_events
		WHERE day_key = ? AND login_42 = ?
		ORDER BY occurred_at
	`, dayKey, login)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []BadgeEvent
	for rows.Next() {
		var occurredAt string
		var doorName string
		if err := rows.Scan(&occurredAt, &doorName); err != nil {
			return nil, err
		}
		ts, err := time.Parse(time.RFC3339Nano, occurredAt)
		if err != nil {
			return nil, err
		}
		events = append(events, BadgeEvent{
			Timestamp: ts,
			DoorName:  doorName,
		})
	}
	return events, rows.Err()
}

func loadHistoricalUsersForDay(dayKey string) ([]User, error) {
	if storageDB == nil || strings.TrimSpace(dayKey) == "" {
		return nil, nil
	}

	rows, err := storageQuery(`
		SELECT control_access_id, control_access_name, login_42, id_42, is_apprentice, profile,
			first_access, last_access, retained_duration_seconds, status, error_message
		FROM watchdog_daily_student_summaries
		WHERE day_key = ?
		ORDER BY login_42
	`, dayKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		var isApprentice int
		var profile int
		var firstAccess sql.NullString
		var lastAccess sql.NullString
		var durationSeconds int64
		var status sql.NullString
		var errorMessage sql.NullString

		if err := rows.Scan(
			&user.ControlAccessID,
			&user.ControlAccessName,
			&user.Login42,
			&user.ID42,
			&isApprentice,
			&profile,
			&firstAccess,
			&lastAccess,
			&durationSeconds,
			&status,
			&errorMessage,
		); err != nil {
			return nil, err
		}

		user.IsApprentice = isApprentice == 1
		user.Profile = ProfileType(profile)
		user.Duration = time.Duration(durationSeconds) * time.Second
		user.Status = status.String
		if firstAccess.Valid {
			user.FirstAccess, err = time.Parse(time.RFC3339Nano, firstAccess.String)
			if err != nil {
				return nil, err
			}
		}
		if lastAccess.Valid {
			user.LastAccess, err = time.Parse(time.RFC3339Nano, lastAccess.String)
			if err != nil {
				return nil, err
			}
		}
		if errorMessage.Valid && strings.TrimSpace(errorMessage.String) != "" {
			user.Error = fmt.Errorf("%s", errorMessage.String)
		}

		users = append(users, user)
	}
	return users, rows.Err()
}

func loadDayAvailabilityForMonth(monthKey string) ([]DayAvailability, error) {
	if storageDB == nil || strings.TrimSpace(monthKey) == "" {
		return nil, nil
	}

	start, err := time.ParseInLocation("2006-01", monthKey, parisLocation())
	if err != nil {
		return nil, err
	}
	end := start.AddDate(0, 1, 0)
	startKey := start.Format("2006-01-02")
	endKey := end.Format("2006-01-02")

	rows, err := storageQuery(`
		SELECT day_key, COUNT(*)
		FROM watchdog_daily_student_summaries
		WHERE day_key >= ? AND day_key < ?
		GROUP BY day_key
		ORDER BY day_key
	`, startKey, endKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDay := make(map[string]DayAvailability)
	for rows.Next() {
		var day DayAvailability
		if err := rows.Scan(&day.DayKey, &day.StudentCount); err != nil {
			return nil, err
		}
		byDay[day.DayKey] = day
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	currentDay := todayDayKeyInParis()
	if strings.HasPrefix(currentDay, monthKey) {
		var count int
		if err := storageQueryRow(`
			SELECT COUNT(*)
			FROM watchdog_current_users
			WHERE day_key = ?
		`, currentDay).Scan(&count); err != nil {
			return nil, err
		}
		if count > 0 {
			byDay[currentDay] = DayAvailability{
				DayKey:       currentDay,
				StudentCount: count,
				Live:         true,
			}
		}
	}

	days := make([]DayAvailability, 0, len(byDay))
	for _, day := range byDay {
		days = append(days, day)
	}
	sort.Slice(days, func(i, j int) bool {
		return days[i].DayKey < days[j].DayKey
	})
	return days, nil
}

func loadHistoricalLocationDaySummaries(login string) (map[string]StudentAttendanceDaySummary, error) {
	if storageDB == nil {
		return nil, nil
	}

	login = normalizeLogin(login)
	fetchedRows, err := storageQuery(`
		SELECT day_key
		FROM watchdog_historical_location_fetches
		WHERE login_42 = ?
	`, login)
	if err != nil {
		return nil, err
	}
	defer fetchedRows.Close()

	fetchedDays := make(map[string]StudentAttendanceDaySummary)
	for fetchedRows.Next() {
		var dayKey string
		if err := fetchedRows.Scan(&dayKey); err != nil {
			return nil, err
		}
		fetchedDays[dayKey] = StudentAttendanceDaySummary{DayKey: dayKey}
	}
	if err := fetchedRows.Err(); err != nil {
		return nil, err
	}

	sessionRows, err := storageQuery(`
		SELECT day_key, begin_at, end_at, host, ongoing
		FROM watchdog_daily_location_sessions
		WHERE login_42 = ?
		ORDER BY day_key, begin_at
	`, login)
	if err != nil {
		return nil, err
	}
	defer sessionRows.Close()

	sessionsByDay := make(map[string][]LocationSession)
	for sessionRows.Next() {
		var dayKey string
		var beginAtRaw string
		var endAtRaw string
		var host string
		var ongoing int
		if err := sessionRows.Scan(&dayKey, &beginAtRaw, &endAtRaw, &host, &ongoing); err != nil {
			return nil, err
		}
		beginAt, err := time.Parse(time.RFC3339Nano, beginAtRaw)
		if err != nil {
			return nil, err
		}
		endAt, err := time.Parse(time.RFC3339Nano, endAtRaw)
		if err != nil {
			return nil, err
		}
		sessionsByDay[dayKey] = append(sessionsByDay[dayKey], LocationSession{
			BeginAt: beginAt,
			EndAt:   endAt,
			Host:    host,
			Ongoing: ongoing == 1,
		})
		if _, ok := fetchedDays[dayKey]; !ok {
			fetchedDays[dayKey] = StudentAttendanceDaySummary{DayKey: dayKey}
		}
	}
	if err := sessionRows.Err(); err != nil {
		return nil, err
	}

	for dayKey, summary := range fetchedDays {
		sessions := sessionsByDay[dayKey]
		summary.Duration = CombinedRetainedDuration(nil, time.Time{}, time.Time{}, sessions)
		if len(sessions) > 0 {
			summary.FirstAccess = sessions[0].BeginAt
			summary.LastAccess = sessions[len(sessions)-1].EndAt
		}
		fetchedDays[dayKey] = summary
	}

	return fetchedDays, nil
}

func enqueueHistoricalLocationFetch(login, dayKey string) {
	login = normalizeLogin(login)
	dayKey = strings.TrimSpace(dayKey)
	if storageDB == nil || login == "" || dayKey == "" || dayKey >= todayDayKeyInParis() {
		return
	}

	key := login + "|" + dayKey

	historicalMonthFetchesMu.Lock()
	if _, ok := historicalMonthFetchesInFlight[key]; ok {
		historicalMonthFetchesMu.Unlock()
		return
	}
	historicalMonthFetchesInFlight[key] = struct{}{}
	historicalMonthFetchesMu.Unlock()

	go func() {
		defer func() {
			historicalMonthFetchesMu.Lock()
			delete(historicalMonthFetchesInFlight, key)
			historicalMonthFetchesMu.Unlock()
		}()

		locationSessions, err := fetchLocationSessionsForDay(login, dayKey)
		if err != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not fetch historical locations for %s on %s: %v", login, dayKey, err))
			return
		}
		if err := replaceHistoricalLocationSessions(dayKey, login, locationSessions); err != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not persist historical locations for %s on %s: %v", login, dayKey, err))
			return
		}
		NotifyLocationSessionsUpdate(login, dayKey)
	}()
}

func monthDayKeys(monthKey string) ([]string, error) {
	start, err := time.ParseInLocation("2006-01", monthKey, parisLocation())
	if err != nil {
		return nil, err
	}
	end := start.AddDate(0, 1, 0)

	days := []string{}
	for cursor := start; cursor.Before(end); cursor = cursor.AddDate(0, 0, 1) {
		days = append(days, cursor.Format("2006-01-02"))
	}
	return days, nil
}

func loadStudentAttendanceDays(login, monthKey string) ([]StudentAttendanceDaySummary, error) {
	if storageDB == nil {
		return nil, nil
	}

	login = normalizeLogin(login)
	rows, err := storageQuery(`
		SELECT day_key, first_access, last_access, retained_duration_seconds, status
		FROM watchdog_daily_student_summaries
		WHERE login_42 = ?
		ORDER BY day_key DESC
	`, login)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dayMap := make(map[string]StudentAttendanceDaySummary)
	for rows.Next() {
		var day StudentAttendanceDaySummary
		var firstAccess sql.NullString
		var lastAccess sql.NullString
		var durationSeconds int64
		var status sql.NullString

		if err := rows.Scan(&day.DayKey, &firstAccess, &lastAccess, &durationSeconds, &status); err != nil {
			return nil, err
		}
		day.Duration = time.Duration(durationSeconds) * time.Second
		day.Status = status.String
		if firstAccess.Valid {
			day.FirstAccess, err = time.Parse(time.RFC3339Nano, firstAccess.String)
			if err != nil {
				return nil, err
			}
		}
		if lastAccess.Valid {
			day.LastAccess, err = time.Parse(time.RFC3339Nano, lastAccess.String)
			if err != nil {
				return nil, err
			}
		}
		dayMap[day.DayKey] = day
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	historicalLocationDays, err := loadHistoricalLocationDaySummaries(login)
	if err != nil {
		return nil, err
	}
	for dayKey, summary := range historicalLocationDays {
		if existing, ok := dayMap[dayKey]; ok {
			if existing.Duration <= 0 && summary.Duration > 0 {
				existing.Duration = summary.Duration
			}
			if existing.FirstAccess.IsZero() && !summary.FirstAccess.IsZero() {
				existing.FirstAccess = summary.FirstAccess
			}
			if existing.LastAccess.IsZero() && !summary.LastAccess.IsZero() {
				existing.LastAccess = summary.LastAccess
			}
			dayMap[dayKey] = existing
			continue
		}
		dayMap[dayKey] = summary
	}

	currentDayKey := currentRuntimeDayKey()
	currentUser, ok, err := loadCurrentUserByLogin(currentDayKey, login)
	if err != nil {
		return nil, err
	}
	currentBadgeEvents, err := loadBadgeEventsForLogin(currentDayKey, login)
	if err != nil {
		return nil, err
	}
	currentLocationSessions, _ := SnapshotDailyLocationSessionsOrSchedule(login)

	currentDaySummary := StudentAttendanceDaySummary{
		DayKey:   currentDayKey,
		Duration: CombinedRetainedDuration(currentBadgeEvents, time.Time{}, time.Time{}, currentLocationSessions),
		Live:     true,
	}
	if ok {
		currentDaySummary.FirstAccess = currentUser.FirstAccess
		currentDaySummary.LastAccess = currentUser.LastAccess
		currentDaySummary.Status = currentUser.Status
		currentDaySummary.Duration = CombinedRetainedDuration(
			currentBadgeEvents,
			currentUser.FirstAccess,
			currentUser.LastAccess,
			currentLocationSessions,
		)
	}
	if len(currentLocationSessions) > 0 {
		if currentDaySummary.FirstAccess.IsZero() {
			currentDaySummary.FirstAccess = currentLocationSessions[0].BeginAt
		}
		if currentDaySummary.LastAccess.IsZero() {
			currentDaySummary.LastAccess = currentLocationSessions[len(currentLocationSessions)-1].EndAt
		}
	}
	dayMap[currentDayKey] = currentDaySummary

	if trimmedMonthKey := strings.TrimSpace(monthKey); trimmedMonthKey != "" {
		dayKeys, err := monthDayKeys(trimmedMonthKey)
		if err != nil {
			return nil, err
		}
		todayKey := todayDayKeyInParis()
		for _, dayKey := range dayKeys {
			if dayKey >= todayKey {
				continue
			}
			if _, ok := dayMap[dayKey]; ok {
				continue
			}
			dayMap[dayKey] = StudentAttendanceDaySummary{
				DayKey:   dayKey,
				Loading:  true,
				Duration: 0,
			}
			enqueueHistoricalLocationFetch(login, dayKey)
		}
	}

	days := make([]StudentAttendanceDaySummary, 0, len(dayMap))
	for _, day := range dayMap {
		days = append(days, day)
	}
	sort.Slice(days, func(i, j int) bool {
		return days[i].DayKey > days[j].DayKey
	})

	return days, nil
}

func loadLatestHistoricalAdminUsers() ([]AdminUserSummary, error) {
	if storageDB == nil {
		return nil, nil
	}

	rows, err := storageQuery(`
		SELECT DISTINCT ON (login_42)
			login_42,
			is_apprentice,
			profile,
			COALESCE(last_access, first_access)
		FROM watchdog_daily_student_summaries
		ORDER BY login_42, COALESCE(last_access, first_access) DESC NULLS LAST, day_key DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []AdminUserSummary
	for rows.Next() {
		var user AdminUserSummary
		var isApprentice int
		var profile int
		var lastBadgeAt sql.NullString
		if err := rows.Scan(&user.Login42, &isApprentice, &profile, &lastBadgeAt); err != nil {
			return nil, err
		}
		user.IsApprentice = isApprentice == 1
		user.Profile = ProfileType(profile)
		if lastBadgeAt.Valid {
			user.LastBadgeAt, err = time.Parse(time.RFC3339Nano, lastBadgeAt.String)
			if err != nil {
				return nil, err
			}
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func loadCurrentAdminUsers() ([]AdminUserSummary, error) {
	if storageDB == nil {
		return nil, nil
	}

	rows, err := storageQuery(`
		SELECT DISTINCT ON (login_42)
			login_42,
			is_apprentice,
			profile,
			COALESCE(last_access, first_access)
		FROM watchdog_current_users
		WHERE day_key = ? AND COALESCE(last_access, first_access) IS NOT NULL
		ORDER BY login_42, COALESCE(last_access, first_access) DESC NULLS LAST, updated_at DESC
	`, currentRuntimeDayKey())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []AdminUserSummary
	for rows.Next() {
		var user AdminUserSummary
		var isApprentice int
		var profile int
		var lastBadgeAt sql.NullString
		if err := rows.Scan(&user.Login42, &isApprentice, &profile, &lastBadgeAt); err != nil {
			return nil, err
		}
		user.IsApprentice = isApprentice == 1
		user.Profile = ProfileType(profile)
		if lastBadgeAt.Valid {
			user.LastBadgeAt, err = time.Parse(time.RFC3339Nano, lastBadgeAt.String)
			if err != nil {
				return nil, err
			}
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func loadActiveLoginsForDay(dayKey string) (map[string]struct{}, error) {
	if storageDB == nil || strings.TrimSpace(dayKey) == "" {
		return nil, nil
	}

	query := `
		SELECT DISTINCT login_42
		FROM watchdog_daily_student_summaries
		WHERE day_key = ?
		UNION
		SELECT DISTINCT login_42
		FROM watchdog_daily_location_sessions
		WHERE day_key = ?
		UNION
		SELECT DISTINCT login_42
		FROM watchdog_badge_events
		WHERE day_key = ?
	`
	args := []any{dayKey, dayKey, dayKey}

	if dayKey == currentRuntimeDayKey() {
		query = `
			SELECT DISTINCT login_42
			FROM watchdog_current_users
			WHERE day_key = ? AND COALESCE(first_access, last_access) IS NOT NULL
			UNION
			SELECT DISTINCT login_42
			FROM watchdog_badge_events
			WHERE day_key = ?
		`
		args = []any{dayKey, dayKey}
	}

	rows, err := storageQuery(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logins := make(map[string]struct{})
	for rows.Next() {
		var login string
		if err := rows.Scan(&login); err != nil {
			return nil, err
		}
		logins[normalizeLogin(login)] = struct{}{}
	}
	return logins, rows.Err()
}

func loadAdminUsers(search string, statuses []string, dayKey string) ([]AdminUserSummary, error) {
	historicalUsers, err := loadLatestHistoricalAdminUsers()
	if err != nil {
		return nil, err
	}
	currentUsers, err := loadCurrentAdminUsers()
	if err != nil {
		return nil, err
	}

	search = normalizeLogin(search)
	requestedStatusFilter := len(statuses) > 0
	statusFilter := make(map[string]struct{}, len(statuses))
	for _, status := range statuses {
		value := normalizeAdminUserStatus(status)
		if value != "" {
			statusFilter[value] = struct{}{}
		}
	}
	if requestedStatusFilter && len(statusFilter) == 0 {
		return []AdminUserSummary{}, nil
	}

	activeLogins := map[string]struct{}(nil)
	if strings.TrimSpace(dayKey) != "" {
		activeLogins, err = loadActiveLoginsForDay(dayKey)
		if err != nil {
			return nil, err
		}
	}

	usersByLogin := make(map[string]AdminUserSummary, len(historicalUsers)+len(currentUsers))
	for _, user := range historicalUsers {
		usersByLogin[normalizeLogin(user.Login42)] = user
	}
	for _, user := range currentUsers {
		usersByLogin[normalizeLogin(user.Login42)] = user
	}

	users := make([]AdminUserSummary, 0, len(usersByLogin))
	for login, user := range usersByLogin {
		if activeLogins != nil {
			if _, ok := activeLogins[login]; !ok {
				continue
			}
		}
		status := adminUserStatus(user.IsApprentice, user.Profile)
		if search != "" && !strings.Contains(normalizeLogin(user.Login42), search) {
			continue
		}
		if requestedStatusFilter {
			if _, ok := statusFilter[status]; !ok {
				continue
			}
		}
		users = append(users, user)
	}

	sort.Slice(users, func(i, j int) bool {
		if users[i].LastBadgeAt.Equal(users[j].LastBadgeAt) {
			return users[i].Login42 < users[j].Login42
		}
		if users[i].LastBadgeAt.IsZero() {
			return false
		}
		if users[j].LastBadgeAt.IsZero() {
			return true
		}
		return users[i].LastBadgeAt.After(users[j].LastBadgeAt)
	})
	return users, nil
}

func loadHistoricalLocationSessions(dayKey, login string) ([]LocationSession, error) {
	if storageDB == nil {
		return nil, nil
	}
	login = strings.ToLower(strings.TrimSpace(login))

	rows, err := storageQuery(`
		SELECT begin_at, end_at, host, ongoing
		FROM watchdog_daily_location_sessions
		WHERE day_key = ? AND login_42 = ?
		ORDER BY begin_at
	`, dayKey, login)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []LocationSession
	for rows.Next() {
		var beginAtRaw string
		var endAtRaw string
		var host string
		var ongoing int
		if err := rows.Scan(&beginAtRaw, &endAtRaw, &host, &ongoing); err != nil {
			return nil, err
		}
		beginAt, err := time.Parse(time.RFC3339Nano, beginAtRaw)
		if err != nil {
			return nil, err
		}
		endAt, err := time.Parse(time.RFC3339Nano, endAtRaw)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, LocationSession{
			BeginAt: beginAt,
			EndAt:   endAt,
			Host:    host,
			Ongoing: ongoing == 1,
		})
	}
	return sessions, rows.Err()
}

func historicalLocationSessionsFetched(dayKey, login string) (bool, error) {
	if storageDB == nil {
		return false, nil
	}
	login = strings.ToLower(strings.TrimSpace(login))

	var fetchedAt string
	err := storageQueryRow(`
		SELECT fetched_at
		FROM watchdog_historical_location_fetches
		WHERE day_key = ? AND login_42 = ?
	`, dayKey, login).Scan(&fetchedAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func replaceHistoricalLocationSessions(dayKey, login string, sessions []LocationSession) (err error) {
	if storageDB == nil {
		return nil
	}
	login = strings.ToLower(strings.TrimSpace(login))

	tx, err := storageDB.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec(rebindPostgres(`DELETE FROM watchdog_daily_location_sessions WHERE day_key = ? AND login_42 = ?`), dayKey, login); err != nil {
		return err
	}

	for _, session := range sessions {
		if _, err = tx.Exec(rebindPostgres(`
			INSERT INTO watchdog_daily_location_sessions (
				day_key, login_42, begin_at, end_at, host, ongoing, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?)
		`),
			dayKey,
			login,
			session.BeginAt.UTC().Format(time.RFC3339Nano),
			session.EndAt.UTC().Format(time.RFC3339Nano),
			session.Host,
			boolToInt(session.Ongoing),
			time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			return err
		}
	}

	if _, err = tx.Exec(rebindPostgres(`
		INSERT INTO watchdog_historical_location_fetches (day_key, login_42, fetched_at)
		VALUES (?, ?, ?)
		ON CONFLICT(day_key, login_42) DO UPDATE SET fetched_at = excluded.fetched_at
	`), dayKey, login, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}

	return tx.Commit()
}

func syntheticHistoricalStudentDay(login, dayKey string, sessions []LocationSession) (HistoricalStudentDay, error) {
	login = normalizeLogin(login)
	record := HistoricalStudentDay{
		DayKey:           dayKey,
		User:             User{Login42: login, Profile: Student},
		BadgeEvents:      []BadgeEvent{},
		LocationSessions: sessions,
		AttendancePosts:  []AttendancePostRecord{},
		RetainedDuration: CombinedRetainedDuration(nil, time.Time{}, time.Time{}, sessions),
		BadgeDuration:    0,
	}

	if summary, ok, err := AdminUserByLogin(login); err != nil {
		return HistoricalStudentDay{}, err
	} else if ok {
		record.User.IsApprentice = summary.IsApprentice
		record.User.Profile = summary.Profile
	}

	return record, nil
}

func saveHistoricalSummary(record HistoricalStudentDay) error {
	if storageDB == nil {
		return nil
	}

	var firstAccess any
	if !record.User.FirstAccess.IsZero() {
		firstAccess = record.User.FirstAccess.UTC().Format(time.RFC3339Nano)
	}
	var lastAccess any
	if !record.User.LastAccess.IsZero() {
		lastAccess = record.User.LastAccess.UTC().Format(time.RFC3339Nano)
	}
	var errorMessage any
	if record.User.Error != nil {
		errorMessage = record.User.Error.Error()
	}

	_, err := storageExec(`
		INSERT INTO watchdog_daily_student_summaries (
			day_key, login_42, control_access_id, control_access_name, id_42, is_apprentice, profile,
			first_access, last_access, badge_duration_seconds, retained_duration_seconds, status, error_message, finalized_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(day_key, login_42) DO UPDATE SET
			control_access_id = excluded.control_access_id,
			control_access_name = excluded.control_access_name,
			id_42 = excluded.id_42,
			is_apprentice = excluded.is_apprentice,
			profile = excluded.profile,
			first_access = excluded.first_access,
			last_access = excluded.last_access,
			badge_duration_seconds = excluded.badge_duration_seconds,
			retained_duration_seconds = excluded.retained_duration_seconds,
			status = excluded.status,
			error_message = excluded.error_message,
			finalized_at = excluded.finalized_at
	`,
		record.DayKey,
		strings.ToLower(strings.TrimSpace(record.User.Login42)),
		record.User.ControlAccessID,
		record.User.ControlAccessName,
		record.User.ID42,
		boolToInt(record.User.IsApprentice),
		int(record.User.Profile),
		firstAccess,
		lastAccess,
		int64(record.BadgeDuration/time.Second),
		int64(record.RetainedDuration/time.Second),
		record.User.Status,
		errorMessage,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func loadHistoricalStudentDay(login, dayKey string) (HistoricalStudentDay, bool, error) {
	if storageDB == nil {
		return HistoricalStudentDay{}, false, nil
	}

	login = strings.ToLower(strings.TrimSpace(login))
	var (
		record          HistoricalStudentDay
		isApprentice    int
		profile         int
		firstAccessRaw  sql.NullString
		lastAccessRaw   sql.NullString
		badgeSeconds    int64
		retainedSeconds int64
		status          sql.NullString
		errorMessage    sql.NullString
	)

	err := storageQueryRow(`
		SELECT control_access_id, control_access_name, id_42, is_apprentice, profile,
			first_access, last_access, badge_duration_seconds, retained_duration_seconds, status, error_message
		FROM watchdog_daily_student_summaries
		WHERE day_key = ? AND login_42 = ?
	`, dayKey, login).Scan(
		&record.User.ControlAccessID,
		&record.User.ControlAccessName,
		&record.User.ID42,
		&isApprentice,
		&profile,
		&firstAccessRaw,
		&lastAccessRaw,
		&badgeSeconds,
		&retainedSeconds,
		&status,
		&errorMessage,
	)
	if err == sql.ErrNoRows {
		return HistoricalStudentDay{}, false, nil
	}
	if err != nil {
		return HistoricalStudentDay{}, false, err
	}

	record.DayKey = dayKey
	record.User.Login42 = login
	record.User.IsApprentice = isApprentice == 1
	record.User.Profile = ProfileType(profile)
	record.User.Status = status.String
	record.BadgeDuration = time.Duration(badgeSeconds) * time.Second
	record.RetainedDuration = time.Duration(retainedSeconds) * time.Second

	if firstAccessRaw.Valid {
		record.User.FirstAccess, err = time.Parse(time.RFC3339Nano, firstAccessRaw.String)
		if err != nil {
			return HistoricalStudentDay{}, false, err
		}
	}
	if lastAccessRaw.Valid {
		record.User.LastAccess, err = time.Parse(time.RFC3339Nano, lastAccessRaw.String)
		if err != nil {
			return HistoricalStudentDay{}, false, err
		}
	}
	if errorMessage.Valid && strings.TrimSpace(errorMessage.String) != "" {
		record.User.Error = fmt.Errorf("%s", errorMessage.String)
	}

	badgeEventsByLogin, err := loadBadgeEventsByLogin(dayKey)
	if err != nil {
		return HistoricalStudentDay{}, false, err
	}
	record.BadgeEvents = badgeEventsByLogin[login]

	record.LocationSessions, err = loadHistoricalLocationSessions(dayKey, login)
	if err != nil {
		return HistoricalStudentDay{}, false, err
	}
	record.AttendancePosts, err = loadAttendancePosts(dayKey, login)
	if err != nil {
		return HistoricalStudentDay{}, false, err
	}
	return record, true, nil
}

func loadAttendancePosts(dayKey, login string) ([]AttendancePostRecord, error) {
	if storageDB == nil {
		return nil, nil
	}
	login = strings.ToLower(strings.TrimSpace(login))

	rows, err := storageQuery(`
		SELECT id_42, control_access_id, begin_at, end_at, payload_json, http_status,
			response_status, error_message, success, created_at
		FROM watchdog_attendance_posts
		WHERE day_key = ? AND login_42 = ?
		ORDER BY created_at
	`, dayKey, login)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AttendancePostRecord
	for rows.Next() {
		var record AttendancePostRecord
		var beginAtRaw sql.NullString
		var endAtRaw sql.NullString
		var payloadRaw string
		var httpStatus sql.NullInt64
		var responseStatus sql.NullString
		var errorMessage sql.NullString
		var success int
		var createdAtRaw string

		if err := rows.Scan(
			&record.ID42,
			&record.ControlAccessID,
			&beginAtRaw,
			&endAtRaw,
			&payloadRaw,
			&httpStatus,
			&responseStatus,
			&errorMessage,
			&success,
			&createdAtRaw,
		); err != nil {
			return nil, err
		}

		record.DayKey = dayKey
		record.Login42 = login
		record.Success = success == 1
		record.ResponseStatus = responseStatus.String
		record.ErrorMessage = errorMessage.String
		record.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAtRaw)
		if err != nil {
			return nil, err
		}
		if beginAtRaw.Valid {
			beginAt, err := time.Parse(time.RFC3339Nano, beginAtRaw.String)
			if err != nil {
				return nil, err
			}
			record.BeginAt = &beginAt
		}
		if endAtRaw.Valid {
			endAt, err := time.Parse(time.RFC3339Nano, endAtRaw.String)
			if err != nil {
				return nil, err
			}
			record.EndAt = &endAt
		}
		if httpStatus.Valid {
			status := int(httpStatus.Int64)
			record.HTTPStatus = &status
		}
		if err := json.Unmarshal([]byte(payloadRaw), &record.Payload); err != nil {
			return nil, err
		}

		out = append(out, record)
	}
	return out, rows.Err()
}

func markDayFinalized(dayKey string) error {
	if storageDB == nil || strings.TrimSpace(dayKey) == "" {
		return nil
	}
	_, err := storageExec(`
		INSERT INTO watchdog_finalized_days (day_key, finalized_at)
		VALUES (?, ?)
		ON CONFLICT(day_key) DO UPDATE SET finalized_at = excluded.finalized_at
	`, dayKey, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func isDayFinalized(dayKey string) (bool, error) {
	if storageDB == nil || strings.TrimSpace(dayKey) == "" {
		return false, nil
	}
	var value string
	err := storageQueryRow(`SELECT day_key FROM watchdog_finalized_days WHERE day_key = ?`, dayKey).Scan(&value)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func finalizeDayWithOverrides(dayKey string, overrides []User) error {
	if storageDB == nil || strings.TrimSpace(dayKey) == "" {
		return nil
	}

	profiles, err := loadDayProfiles(dayKey)
	if err != nil {
		return err
	}
	currentUsers, err := loadCurrentUsersForDay(dayKey)
	if err != nil {
		return err
	}
	badgeEventsByLogin, err := loadBadgeEventsByLogin(dayKey)
	if err != nil {
		return err
	}

	usersByLogin := make(map[string]User, len(profiles)+len(currentUsers)+len(overrides))
	for login, user := range profiles {
		usersByLogin[login] = user
	}
	for _, user := range currentUsers {
		usersByLogin[strings.ToLower(strings.TrimSpace(user.Login42))] = user
	}
	for _, user := range overrides {
		usersByLogin[strings.ToLower(strings.TrimSpace(user.Login42))] = user
	}

	loginSet := make(map[string]struct{}, len(usersByLogin)+len(badgeEventsByLogin))
	for login := range usersByLogin {
		loginSet[login] = struct{}{}
	}
	for login := range badgeEventsByLogin {
		loginSet[login] = struct{}{}
	}

	logins := make([]string, 0, len(loginSet))
	for login := range loginSet {
		logins = append(logins, login)
	}
	sort.Strings(logins)

	for _, login := range logins {
		user, ok := usersByLogin[login]
		if !ok {
			continue
		}
		user.Login42 = login

		locationSessions, err := fetchLocationSessionsForDay(login, dayKey)
		if err != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not fetch historical locations for %s on %s: %v", login, dayKey, err))
			locationSessions = nil
		}
		if err := replaceHistoricalLocationSessions(dayKey, login, locationSessions); err != nil {
			return err
		}

		record := HistoricalStudentDay{
			DayKey:           dayKey,
			User:             user,
			BadgeEvents:      badgeEventsByLogin[login],
			LocationSessions: locationSessions,
			RetainedDuration: CombinedRetainedDuration(badgeEventsByLogin[login], user.FirstAccess, user.LastAccess, locationSessions),
			BadgeDuration:    user.Duration,
		}
		if err := saveHistoricalSummary(record); err != nil {
			return err
		}
	}

	return markDayFinalized(dayKey)
}

func HistoricalStudentDayByLogin(login, dayKey string) (HistoricalStudentDay, bool, error) {
	ensureRuntimeDayState()
	record, ok, err := loadHistoricalStudentDay(login, dayKey)
	if err != nil || ok {
		return record, ok, err
	}

	cachedSessions, err := loadHistoricalLocationSessions(dayKey, login)
	if err != nil {
		return HistoricalStudentDay{}, false, err
	}
	if fetched, err := historicalLocationSessionsFetched(dayKey, login); err != nil {
		return HistoricalStudentDay{}, false, err
	} else if fetched {
		record, err := syntheticHistoricalStudentDay(login, dayKey, cachedSessions)
		if err != nil {
			return HistoricalStudentDay{}, false, err
		}
		return record, true, nil
	}

	locationSessions, err := fetchLocationSessionsForDay(login, dayKey)
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not fetch historical locations for %s on %s: %v", login, dayKey, err))
		record, synthErr := syntheticHistoricalStudentDay(login, dayKey, nil)
		if synthErr != nil {
			return HistoricalStudentDay{}, false, synthErr
		}
		return record, true, nil
	}
	if err := replaceHistoricalLocationSessions(dayKey, login, locationSessions); err != nil {
		return HistoricalStudentDay{}, false, err
	}
	NotifyLocationSessionsUpdate(login, dayKey)

	record, err = syntheticHistoricalStudentDay(login, dayKey, locationSessions)
	if err != nil {
		return HistoricalStudentDay{}, false, err
	}
	return record, true, nil
}

func CurrentUsers() ([]User, error) {
	ensureRuntimeDayState()
	return loadCurrentUsersForDay(currentRuntimeDayKey())
}

func CurrentUserByLogin(login string) (User, bool, error) {
	ensureRuntimeDayState()
	return loadCurrentUserByLogin(currentRuntimeDayKey(), login)
}

func CurrentBadgeEvents(login string) ([]BadgeEvent, error) {
	ensureRuntimeDayState()
	return loadBadgeEventsForLogin(currentRuntimeDayKey(), login)
}

func UsersForDay(dayKey string) ([]User, bool, error) {
	ensureRuntimeDayState()
	if strings.TrimSpace(dayKey) == "" || dayKey == currentRuntimeDayKey() {
		users, err := loadCurrentUsersForDay(currentRuntimeDayKey())
		return users, true, err
	}
	users, err := loadHistoricalUsersForDay(dayKey)
	return users, false, err
}

func DayAvailabilityForMonth(monthKey string) ([]DayAvailability, error) {
	ensureRuntimeDayState()
	return loadDayAvailabilityForMonth(monthKey)
}

func AttendanceDaysForLogin(login, monthKey string) ([]StudentAttendanceDaySummary, error) {
	ensureRuntimeDayState()
	return loadStudentAttendanceDays(login, monthKey)
}

func AdminUsers(search string, statuses []string, dayKey string) ([]AdminUserSummary, error) {
	ensureRuntimeDayState()
	return loadAdminUsers(search, statuses, dayKey)
}

func AdminUserByLogin(login string) (AdminUserSummary, bool, error) {
	users, err := AdminUsers(login, nil, "")
	if err != nil {
		return AdminUserSummary{}, false, err
	}
	for _, user := range users {
		if normalizeLogin(user.Login42) == normalizeLogin(login) {
			return user, true, nil
		}
	}
	return AdminUserSummary{}, false, nil
}

func normalizeLogin(login string) string {
	return strings.ToLower(strings.TrimSpace(login))
}

func normalizeAdminUserStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "student", "etudiant":
		return "student"
	case "apprentice", "alternant":
		return "apprentice"
	case "pisciner", "piscineux":
		return "pisciner"
	default:
		return ""
	}
}

func adminUserStatus(isApprentice bool, profile ProfileType) string {
	if isApprentice {
		return "apprentice"
	}
	if profile == Pisciner {
		return "pisciner"
	}
	return "student"
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
