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
	CalendarDay      StudentCalendarDay     `json:"calendar_day"`
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

type DailyReportSummary struct {
	DayKey       string `json:"day_key"`
	StudentCount int    `json:"student_count"`
	PostedCount  int    `json:"posted_count"`
	FailedCount  int    `json:"failed_count"`
	Live         bool   `json:"live"`
}

type StudentAttendanceDaySummary struct {
	DayKey                  string        `json:"day_key"`
	FirstAccess             time.Time     `json:"first_access"`
	LastAccess              time.Time     `json:"last_access"`
	Duration                time.Duration `json:"duration"`
	Status                  string        `json:"status"`
	Live                    bool          `json:"live"`
	Loading                 bool          `json:"loading"`
	DayType                 string        `json:"day_type"`
	DayTypeLabel            string        `json:"day_type_label"`
	RequiredAttendanceHours *float64      `json:"required_attendance_hours"`
}

type AdminUserSummary struct {
	Login42          string        `json:"login_42"`
	IsApprentice     bool          `json:"is_apprentice"`
	Profile          ProfileType   `json:"profile"`
	Status           string        `json:"status"`
	Status42         string        `json:"status_42"`
	StatusOverridden bool          `json:"status_overridden"`
	IsBlacklisted    bool          `json:"is_blacklisted"`
	BadgePostingOff  bool          `json:"badge_posting_off"`
	BlacklistReason  string        `json:"blacklist_reason"`
	LastBadgeAt      time.Time     `json:"last_badge_at"`
	DayDuration      time.Duration `json:"day_duration"`
}

type historicalMonthFetchTask struct {
	Login    string
	MonthKey string
	DayKeys  []string
}

var (
	storageDB                       *sql.DB
	storageRuntimeDay               string
	storageDayMutex                 sync.Mutex
	historicalMonthFetchesMu        sync.Mutex
	historicalMonthFetchesInFlight  = map[string]struct{}{}
	historicalMonthFetchQueue       chan historicalMonthFetchTask
	historicalMonthFetchWorkersOnce sync.Once
)

const storageSchema = `
CREATE TABLE IF NOT EXISTS watchdog_users (
	login_42 TEXT PRIMARY KEY,
	id_42 TEXT NOT NULL DEFAULT '',
	control_access_id INTEGER NOT NULL DEFAULT 0,
	control_access_name TEXT NOT NULL DEFAULT '',
	is_apprentice INTEGER NOT NULL DEFAULT 0,
	profile INTEGER NOT NULL DEFAULT 4,
	status TEXT NOT NULL DEFAULT 'student',
	status_42 TEXT NOT NULL DEFAULT 'student',
	status_overridden INTEGER NOT NULL DEFAULT 0,
	is_blacklisted INTEGER NOT NULL DEFAULT 0,
	badge_posting_off INTEGER NOT NULL DEFAULT 0,
	blacklist_reason TEXT NOT NULL DEFAULT '',
	photo_url TEXT NOT NULL DEFAULT '',
	photo_fetched_at TEXT,
	last_status_checked_at TEXT,
	updated_at TEXT NOT NULL,
	created_at TEXT NOT NULL
);

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
	day_type TEXT NOT NULL DEFAULT '',
	day_type_label TEXT NOT NULL DEFAULT '',
	required_attendance_hours DOUBLE PRECISION,
	status TEXT NOT NULL DEFAULT '',
	post_result TEXT NOT NULL DEFAULT '',
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

CREATE TABLE IF NOT EXISTS watchdog_daily_attendance_bounds (
	day_key TEXT NOT NULL,
	login_42 TEXT NOT NULL,
	begin_at TEXT NOT NULL,
	end_at TEXT NOT NULL,
	source TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	PRIMARY KEY (day_key, login_42)
);

CREATE TABLE IF NOT EXISTS watchdog_historical_attendance_fetches (
	day_key TEXT NOT NULL,
	login_42 TEXT NOT NULL,
	fetched_at TEXT NOT NULL,
	PRIMARY KEY (day_key, login_42)
);

CREATE TABLE IF NOT EXISTS watchdog_cfa_training_ids (
	login_42 TEXT PRIMARY KEY,
	training_id INTEGER NOT NULL DEFAULT 0,
	refreshed_month TEXT NOT NULL,
	updated_at TEXT NOT NULL
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
`

const (
	historicalMonthFetchWorkerCount = 4
	historicalMonthFetchQueueSize   = 32
)

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
	if err := migrateStorageSchema(); err != nil {
		_ = db.Close()
		storageDB = nil
		return fmt.Errorf("migrate postgres schema: %w", err)
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

func storageTableExists(name string) (bool, error) {
	if storageDB == nil {
		return false, nil
	}

	var exists bool
	err := storageQueryRow(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = ?
		)
	`, strings.TrimSpace(name)).Scan(&exists)
	return exists, err
}

func migrateStorageSchema() error {
	if storageDB == nil {
		return nil
	}

	if err := migrateUsersTable(); err != nil {
		return err
	}
	if err := migrateCurrentUsersTable(); err != nil {
		return err
	}
	if err := migrateDailyStudentSummariesTable(); err != nil {
		return err
	}

	legacyTables := []string{
		"watchdog_profile_photos",
		"watchdog_student_status_state",
		"watchdog_student_status_history",
		"watchdog_user_settings",
	}
	for _, tableName := range legacyTables {
		if _, err := storageExec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tableName)); err != nil {
			return err
		}
	}
	return nil
}

func migrateDailyStudentSummariesTable() error {
	if storageDB == nil {
		return nil
	}

	alterStatements := []string{
		`ALTER TABLE watchdog_daily_student_summaries ADD COLUMN IF NOT EXISTS post_result TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE watchdog_daily_student_summaries ADD COLUMN IF NOT EXISTS day_type TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE watchdog_daily_student_summaries ADD COLUMN IF NOT EXISTS day_type_label TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE watchdog_daily_student_summaries ADD COLUMN IF NOT EXISTS required_attendance_hours DOUBLE PRECISION`,
	}
	for _, statement := range alterStatements {
		if _, err := storageExec(statement); err != nil {
			return err
		}
	}

	_, err := storageExec(`
		UPDATE watchdog_daily_student_summaries
		SET
			post_result = CASE
				WHEN TRIM(post_result) = '' AND TRIM(status) NOT IN ('student', 'apprentice', 'pisciner', 'staff', 'extern') THEN status
				ELSE post_result
			END,
			status = CASE
				WHEN TRIM(status) NOT IN ('student', 'apprentice', 'pisciner', 'staff', 'extern') THEN
					CASE
						WHEN is_apprentice = 1 THEN 'apprentice'
						WHEN profile = 2 THEN 'pisciner'
						WHEN profile = 1 THEN 'staff'
						WHEN profile = 4 THEN 'student'
						ELSE 'extern'
					END
				ELSE status
			END
	`)
	return err
}

func migrateCurrentUsersTable() error {
	if storageDB == nil {
		return nil
	}

	dropStatements := []string{
		`ALTER TABLE watchdog_current_users DROP COLUMN IF EXISTS status`,
		`ALTER TABLE watchdog_current_users DROP COLUMN IF EXISTS post_result`,
		`ALTER TABLE watchdog_current_users DROP COLUMN IF EXISTS error_message`,
	}
	for _, statement := range dropStatements {
		if _, err := storageExec(statement); err != nil {
			return err
		}
	}

	if _, err := storageExec(`ALTER TABLE watchdog_current_users ADD COLUMN IF NOT EXISTS is_apprentice INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if _, err := storageExec(`ALTER TABLE watchdog_current_users ADD COLUMN IF NOT EXISTS profile INTEGER NOT NULL DEFAULT 4`); err != nil {
		return err
	}
	return nil
}

func migrateUsersTable() error {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	alterStatements := []string{
		`ALTER TABLE watchdog_users ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'student'`,
		`ALTER TABLE watchdog_users ADD COLUMN IF NOT EXISTS status_42 TEXT NOT NULL DEFAULT 'student'`,
		`ALTER TABLE watchdog_users ADD COLUMN IF NOT EXISTS status_overridden INTEGER NOT NULL DEFAULT 0`,
	}
	for _, statement := range alterStatements {
		if _, err := storageExec(statement); err != nil {
			return err
		}
	}

	seedStatements := []string{
		`
		INSERT INTO watchdog_users (
			login_42, id_42, control_access_id, control_access_name, is_apprentice, profile, status, status_42, updated_at, created_at
		)
		SELECT
			login_42,
			id_42,
			control_access_id,
			control_access_name,
			is_apprentice,
			profile,
			CASE
				WHEN is_apprentice = 1 THEN 'apprentice'
				WHEN profile = 2 THEN 'pisciner'
				WHEN profile = 1 THEN 'staff'
				WHEN profile = 4 THEN 'student'
				ELSE 'extern'
			END,
			CASE
				WHEN is_apprentice = 1 THEN 'apprentice'
				WHEN profile = 2 THEN 'pisciner'
				WHEN profile = 1 THEN 'staff'
				WHEN profile = 4 THEN 'student'
				ELSE 'extern'
			END,
			?,
			?
		FROM (
			SELECT DISTINCT ON (login_42)
				login_42, id_42, control_access_id, control_access_name, is_apprentice, profile
			FROM watchdog_daily_student_summaries
			WHERE login_42 <> ''
			ORDER BY login_42, finalized_at DESC, day_key DESC
		) AS summaries
		ON CONFLICT (login_42) DO NOTHING
		`,
		`
		INSERT INTO watchdog_users (
			login_42, id_42, control_access_id, control_access_name, is_apprentice, profile, status, status_42, updated_at, created_at
		)
		SELECT
			login_42,
			id_42,
			control_access_id,
			control_access_name,
			is_apprentice,
			profile,
			CASE
				WHEN is_apprentice = 1 THEN 'apprentice'
				WHEN profile = 2 THEN 'pisciner'
				WHEN profile = 1 THEN 'staff'
				WHEN profile = 4 THEN 'student'
				ELSE 'extern'
			END,
			CASE
				WHEN is_apprentice = 1 THEN 'apprentice'
				WHEN profile = 2 THEN 'pisciner'
				WHEN profile = 1 THEN 'staff'
				WHEN profile = 4 THEN 'student'
				ELSE 'extern'
			END,
			?,
			?
		FROM (
			SELECT DISTINCT ON (login_42)
				login_42, id_42, control_access_id, control_access_name, is_apprentice, profile
			FROM watchdog_day_profiles
			WHERE login_42 <> ''
			ORDER BY login_42, updated_at DESC, day_key DESC
		) AS profiles
		ON CONFLICT (login_42) DO UPDATE SET
			id_42 = CASE WHEN excluded.id_42 <> '' THEN excluded.id_42 ELSE watchdog_users.id_42 END,
			control_access_id = CASE WHEN excluded.control_access_id <> 0 THEN excluded.control_access_id ELSE watchdog_users.control_access_id END,
			control_access_name = CASE WHEN excluded.control_access_name <> '' THEN excluded.control_access_name ELSE watchdog_users.control_access_name END,
			is_apprentice = excluded.is_apprentice,
			profile = excluded.profile,
			status_42 = excluded.status_42,
			status = CASE WHEN watchdog_users.status_overridden = 1 THEN watchdog_users.status ELSE excluded.status END,
			updated_at = excluded.updated_at
		`,
		`
		INSERT INTO watchdog_users (
			login_42, id_42, control_access_id, control_access_name, is_apprentice, profile, status, status_42, updated_at, created_at
		)
		SELECT
			login_42,
			id_42,
			control_access_id,
			control_access_name,
			is_apprentice,
			profile,
			CASE
				WHEN is_apprentice = 1 THEN 'apprentice'
				WHEN profile = 2 THEN 'pisciner'
				WHEN profile = 1 THEN 'staff'
				WHEN profile = 4 THEN 'student'
				ELSE 'extern'
			END,
			CASE
				WHEN is_apprentice = 1 THEN 'apprentice'
				WHEN profile = 2 THEN 'pisciner'
				WHEN profile = 1 THEN 'staff'
				WHEN profile = 4 THEN 'student'
				ELSE 'extern'
			END,
			?,
			?
		FROM (
			SELECT DISTINCT ON (login_42)
				login_42, id_42, control_access_id, control_access_name, is_apprentice, profile
			FROM watchdog_current_users
			WHERE login_42 <> ''
			ORDER BY login_42, updated_at DESC, day_key DESC
		) AS current_users
		ON CONFLICT (login_42) DO UPDATE SET
			id_42 = CASE WHEN excluded.id_42 <> '' THEN excluded.id_42 ELSE watchdog_users.id_42 END,
			control_access_id = CASE WHEN excluded.control_access_id <> 0 THEN excluded.control_access_id ELSE watchdog_users.control_access_id END,
			control_access_name = CASE WHEN excluded.control_access_name <> '' THEN excluded.control_access_name ELSE watchdog_users.control_access_name END,
			is_apprentice = excluded.is_apprentice,
			profile = excluded.profile,
			status_42 = excluded.status_42,
			status = CASE WHEN watchdog_users.status_overridden = 1 THEN watchdog_users.status ELSE excluded.status END,
			updated_at = excluded.updated_at
		`,
	}

	for _, statement := range seedStatements {
		if _, err := storageExec(statement, now, now); err != nil {
			return err
		}
	}

	if exists, err := storageTableExists("watchdog_user_settings"); err != nil {
		return err
	} else if exists {
		if _, err := storageExec(`
			UPDATE watchdog_users AS users
			SET
				is_blacklisted = settings.is_blacklisted,
				badge_posting_off = settings.badge_posting_off,
				blacklist_reason = settings.blacklist_reason,
				updated_at = settings.updated_at
			FROM watchdog_user_settings AS settings
			WHERE settings.login_42 = users.login_42
		`); err != nil {
			return err
		}
		if _, err := storageExec(`
			INSERT INTO watchdog_users (
				login_42, is_blacklisted, badge_posting_off, blacklist_reason, updated_at, created_at
			)
			SELECT login_42, is_blacklisted, badge_posting_off, blacklist_reason, updated_at, updated_at
			FROM watchdog_user_settings
			ON CONFLICT (login_42) DO UPDATE SET
				is_blacklisted = excluded.is_blacklisted,
				badge_posting_off = excluded.badge_posting_off,
				blacklist_reason = excluded.blacklist_reason,
				updated_at = excluded.updated_at
		`); err != nil {
			return err
		}
	}

	if exists, err := storageTableExists("watchdog_profile_photos"); err != nil {
		return err
	} else if exists {
		if _, err := storageExec(`
			UPDATE watchdog_users AS users
			SET
				photo_url = photos.photo_url,
				photo_fetched_at = photos.fetched_at,
				updated_at = photos.updated_at
			FROM watchdog_profile_photos AS photos
			WHERE photos.login_42 = users.login_42
		`); err != nil {
			return err
		}
		if _, err := storageExec(`
			INSERT INTO watchdog_users (
				login_42, photo_url, photo_fetched_at, updated_at, created_at
			)
			SELECT login_42, photo_url, fetched_at, updated_at, updated_at
			FROM watchdog_profile_photos
			ON CONFLICT (login_42) DO UPDATE SET
				photo_url = excluded.photo_url,
				photo_fetched_at = excluded.photo_fetched_at,
				updated_at = excluded.updated_at
		`); err != nil {
			return err
		}
	}

	if exists, err := storageTableExists("watchdog_student_status_state"); err != nil {
		return err
	} else if exists {
		if _, err := storageExec(`
			UPDATE watchdog_users AS users
			SET
				is_apprentice = statuses.is_apprentice,
				last_status_checked_at = statuses.last_checked_at,
				updated_at = statuses.updated_at
			FROM watchdog_student_status_state AS statuses
			WHERE statuses.login_42 = users.login_42
		`); err != nil {
			return err
		}
		if _, err := storageExec(`
			INSERT INTO watchdog_users (
				login_42, is_apprentice, last_status_checked_at, updated_at, created_at
			)
			SELECT login_42, is_apprentice, last_checked_at, updated_at, updated_at
			FROM watchdog_student_status_state
			ON CONFLICT (login_42) DO UPDATE SET
				is_apprentice = excluded.is_apprentice,
				last_status_checked_at = excluded.last_status_checked_at,
				updated_at = excluded.updated_at
		`); err != nil {
			return err
		}
	}

	if _, err := storageExec(`
		UPDATE watchdog_users
		SET
			status_42 = CASE
				WHEN is_apprentice = 1 THEN 'apprentice'
				WHEN profile = 2 THEN 'pisciner'
				WHEN profile = 1 THEN 'staff'
				WHEN profile = 4 THEN 'student'
				ELSE 'extern'
			END,
			status = CASE
				WHEN status_overridden = 1 AND TRIM(status) <> '' THEN status
				ELSE CASE
					WHEN is_apprentice = 1 THEN 'apprentice'
					WHEN profile = 2 THEN 'pisciner'
					WHEN profile = 1 THEN 'staff'
					WHEN profile = 4 THEN 'student'
					ELSE 'extern'
				END
			END
	`); err != nil {
		return err
	}

	return nil
}

func saveUserIdentity(user User) error {
	if storageDB == nil {
		return nil
	}

	login := normalizeLogin(user.Login42)
	if login == "" {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	detectedStatus := statusFromSignals(user.IsApprentice, user.Profile)
	_, err := storageExec(`
		INSERT INTO watchdog_users (
			login_42, id_42, control_access_id, control_access_name, is_apprentice, profile, status, status_42, updated_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(login_42) DO UPDATE SET
			id_42 = CASE WHEN excluded.id_42 <> '' THEN excluded.id_42 ELSE watchdog_users.id_42 END,
			control_access_id = CASE WHEN excluded.control_access_id <> 0 THEN excluded.control_access_id ELSE watchdog_users.control_access_id END,
			control_access_name = CASE WHEN excluded.control_access_name <> '' THEN excluded.control_access_name ELSE watchdog_users.control_access_name END,
			is_apprentice = excluded.is_apprentice,
			profile = excluded.profile,
			status_42 = excluded.status_42,
			status = CASE
				WHEN watchdog_users.status_overridden = 1 AND watchdog_users.status_42 = excluded.status_42 THEN watchdog_users.status
				ELSE excluded.status
			END,
			updated_at = excluded.updated_at
	`,
		login,
		strings.TrimSpace(user.ID42),
		user.ControlAccessID,
		strings.TrimSpace(user.ControlAccessName),
		boolToInt(user.IsApprentice),
		int(user.Profile),
		detectedStatus,
		detectedStatus,
		now,
		now,
	)
	return err
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
	if err := saveUserIdentity(user); err != nil {
		return err
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
	if err := saveUserIdentity(user); err != nil {
		return err
	}

	var firstAccess any
	if !user.FirstAccess.IsZero() {
		firstAccess = user.FirstAccess.UTC().Format(time.RFC3339Nano)
	}
	var lastAccess any
	if !user.LastAccess.IsZero() {
		lastAccess = user.LastAccess.UTC().Format(time.RFC3339Nano)
	}
	_, err := storageExec(`
		INSERT INTO watchdog_current_users (
			day_key, control_access_id, control_access_name, login_42, id_42, is_apprentice, profile,
			first_access, last_access, duration_seconds, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(day_key, control_access_id) DO UPDATE SET
			control_access_name = excluded.control_access_name,
			login_42 = excluded.login_42,
			id_42 = excluded.id_42,
			is_apprentice = excluded.is_apprentice,
			profile = excluded.profile,
			first_access = excluded.first_access,
			last_access = excluded.last_access,
			duration_seconds = excluded.duration_seconds,
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

func loadCurrentUsersForDay(dayKey string, apprenticesOnly bool) ([]User, error) {
	if storageDB == nil || strings.TrimSpace(dayKey) == "" {
		return nil, nil
	}

	query := `
		SELECT control_access_id, control_access_name, login_42, id_42, is_apprentice, profile,
			first_access, last_access, duration_seconds
		FROM watchdog_current_users
		WHERE day_key = ?
	`
	args := []any{dayKey}
	if apprenticesOnly {
		query += ` AND is_apprentice = 1`
	}

	rows, err := storageQuery(query, args...)
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
		); err != nil {
			return nil, err
		}

		user.IsApprentice = isApprentice == 1
		user.Profile = ProfileType(profile)
		user.Duration = time.Duration(durationSeconds) * time.Second
		user.BadgeDuration = user.Duration
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
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	logins := make([]string, 0, len(users))
	for _, user := range users {
		logins = append(logins, user.Login42)
	}
	settingsByLogin, err := loadUserSettingsMap(logins)
	if err != nil {
		return nil, err
	}
	for index := range users {
		if settings, ok := settingsByLogin[normalizeLogin(users[index].Login42)]; ok {
			applyUserSettings(&users[index], settings)
		} else {
			users[index].Status42 = statusFromSignals(users[index].IsApprentice, users[index].Profile)
			users[index].Status = users[index].Status42
		}
	}
	return users, nil
}

type studentStatusStateRecord struct {
	Login42       string
	IsApprentice  bool
	LastCheckedAt time.Time
}

type UserSettings struct {
	Login42          string `json:"login_42"`
	Status           string `json:"status"`
	Status42         string `json:"status_42"`
	StatusOverridden bool   `json:"status_overridden"`
	IsBlacklisted    bool   `json:"is_blacklisted"`
	BadgePostingOff  bool   `json:"badge_posting_off"`
	BlacklistReason  string `json:"blacklist_reason,omitempty"`
}

func loadUserSettings(login string) (UserSettings, bool, error) {
	if storageDB == nil {
		return UserSettings{}, false, nil
	}

	login = normalizeLogin(login)
	var (
		settings         UserSettings
		statusOverridden int
		isBlacklisted    int
		badgePostingOff  int
	)

	err := storageQueryRow(`
		SELECT login_42, status, status_42, status_overridden, is_blacklisted, badge_posting_off, blacklist_reason
		FROM watchdog_users
		WHERE login_42 = ?
	`, login).Scan(&settings.Login42, &settings.Status, &settings.Status42, &statusOverridden, &isBlacklisted, &badgePostingOff, &settings.BlacklistReason)
	if err == sql.ErrNoRows {
		return UserSettings{}, false, nil
	}
	if err != nil {
		return UserSettings{}, false, err
	}

	settings.Status = normalizeManagedUserStatus(settings.Status)
	settings.Status42 = normalizeManagedUserStatus(settings.Status42)
	settings.StatusOverridden = statusOverridden == 1
	settings.IsBlacklisted = isBlacklisted == 1
	settings.BadgePostingOff = badgePostingOff == 1
	settings.BlacklistReason = strings.TrimSpace(settings.BlacklistReason)
	return settings, true, nil
}

func loadUserSettingsMap(logins []string) (map[string]UserSettings, error) {
	if storageDB == nil || len(logins) == 0 {
		return map[string]UserSettings{}, nil
	}

	seen := make(map[string]struct{}, len(logins))
	uniqueLogins := make([]string, 0, len(logins))
	for _, login := range logins {
		login = normalizeLogin(login)
		if login == "" {
			continue
		}
		if _, ok := seen[login]; ok {
			continue
		}
		seen[login] = struct{}{}
		uniqueLogins = append(uniqueLogins, login)
	}
	if len(uniqueLogins) == 0 {
		return map[string]UserSettings{}, nil
	}

	placeholders := make([]string, 0, len(uniqueLogins))
	args := make([]any, 0, len(uniqueLogins))
	for _, login := range uniqueLogins {
		placeholders = append(placeholders, "?")
		args = append(args, login)
	}

	rows, err := storageQuery(fmt.Sprintf(`
		SELECT login_42, status, status_42, status_overridden, is_blacklisted, badge_posting_off, blacklist_reason
		FROM watchdog_users
		WHERE login_42 IN (%s)
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settingsByLogin := make(map[string]UserSettings, len(uniqueLogins))
	for rows.Next() {
		var (
			settings         UserSettings
			statusOverridden int
			isBlacklisted    int
			badgePostingOff  int
		)
		if err := rows.Scan(&settings.Login42, &settings.Status, &settings.Status42, &statusOverridden, &isBlacklisted, &badgePostingOff, &settings.BlacklistReason); err != nil {
			return nil, err
		}
		settings.Status = normalizeManagedUserStatus(settings.Status)
		settings.Status42 = normalizeManagedUserStatus(settings.Status42)
		settings.StatusOverridden = statusOverridden == 1
		settings.IsBlacklisted = isBlacklisted == 1
		settings.BadgePostingOff = badgePostingOff == 1
		settings.BlacklistReason = strings.TrimSpace(settings.BlacklistReason)
		settingsByLogin[normalizeLogin(settings.Login42)] = settings
	}
	return settingsByLogin, rows.Err()
}

func saveUserSettings(settings UserSettings) error {
	if storageDB == nil {
		return nil
	}

	settings.Login42 = normalizeLogin(settings.Login42)
	if settings.Login42 == "" {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := storageExec(`
		INSERT INTO watchdog_users (
			login_42, status, status_42, status_overridden, is_blacklisted, badge_posting_off, blacklist_reason, updated_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(login_42) DO UPDATE SET
			status = excluded.status,
			status_42 = excluded.status_42,
			status_overridden = excluded.status_overridden,
			is_blacklisted = excluded.is_blacklisted,
			badge_posting_off = excluded.badge_posting_off,
			blacklist_reason = excluded.blacklist_reason,
			updated_at = excluded.updated_at
	`,
		settings.Login42,
		coalesceManagedStatus(settings.Status, settings.Status42),
		coalesceManagedStatus(settings.Status42, settings.Status),
		boolToInt(settings.StatusOverridden),
		boolToInt(settings.IsBlacklisted),
		boolToInt(settings.BadgePostingOff),
		strings.TrimSpace(settings.BlacklistReason),
		now,
		now,
	)
	return err
}

func applyUserSettings(user *User, settings UserSettings) {
	if user == nil {
		return
	}
	detectedStatus := statusFromSignals(user.IsApprentice, user.Profile)
	user.Status42 = coalesceManagedStatus(settings.Status42, detectedStatus)
	user.Status = user.Status42
	user.StatusOverridden = false
	if settings.StatusOverridden {
		if overriddenStatus := coalesceManagedStatus(settings.Status, user.Status42); overriddenStatus != "" {
			user.Status = overriddenStatus
			user.StatusOverridden = overriddenStatus != user.Status42
			user.IsApprentice, user.Profile = signalsFromStatus(overriddenStatus)
		}
	}
	user.IsBlacklisted = settings.IsBlacklisted
	user.BadgePostingOff = settings.BadgePostingOff
	user.BlacklistReason = settings.BlacklistReason
}

func loadStudentStatusState(login string) (studentStatusStateRecord, bool, error) {
	if storageDB == nil {
		return studentStatusStateRecord{}, false, nil
	}

	login = normalizeLogin(login)
	var (
		record         studentStatusStateRecord
		isApprentice   int
		lastCheckedRaw sql.NullString
	)

	err := storageQueryRow(`
		SELECT login_42, is_apprentice, last_status_checked_at
		FROM watchdog_users
		WHERE login_42 = ?
	`, login).Scan(&record.Login42, &isApprentice, &lastCheckedRaw)
	if err == sql.ErrNoRows {
		return studentStatusStateRecord{}, false, nil
	}
	if err != nil {
		return studentStatusStateRecord{}, false, err
	}

	record.IsApprentice = isApprentice == 1
	if lastCheckedRaw.Valid && strings.TrimSpace(lastCheckedRaw.String) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, lastCheckedRaw.String)
		if err != nil {
			return studentStatusStateRecord{}, false, err
		}
		record.LastCheckedAt = parsed
	}

	return record, true, nil
}

func saveStudentStatusState(login string, isApprentice bool, checkedAt time.Time, fallbackPrevious *bool) error {
	if storageDB == nil {
		return nil
	}
	_ = fallbackPrevious

	login = normalizeLogin(login)
	if login == "" {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	var checkedAtValue any
	if !checkedAt.IsZero() {
		checkedAtValue = checkedAt.UTC().Format(time.RFC3339Nano)
	}
	detectedStatus := statusFromSignals(isApprentice, Student)

	_, err := storageExec(`
		INSERT INTO watchdog_users (login_42, is_apprentice, status, status_42, last_status_checked_at, updated_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(login_42) DO UPDATE SET
			is_apprentice = excluded.is_apprentice,
			status_42 = CASE
				WHEN excluded.is_apprentice = 1 THEN 'apprentice'
				WHEN watchdog_users.profile = 2 THEN 'pisciner'
				WHEN watchdog_users.profile = 1 THEN 'staff'
				WHEN watchdog_users.profile = 4 THEN 'student'
				ELSE 'extern'
			END,
			status = CASE
				WHEN watchdog_users.status_overridden = 1 AND watchdog_users.status_42 = CASE
					WHEN excluded.is_apprentice = 1 THEN 'apprentice'
					WHEN watchdog_users.profile = 2 THEN 'pisciner'
					WHEN watchdog_users.profile = 1 THEN 'staff'
					WHEN watchdog_users.profile = 4 THEN 'student'
					ELSE 'extern'
				END THEN watchdog_users.status
				ELSE CASE
					WHEN excluded.is_apprentice = 1 THEN 'apprentice'
					WHEN watchdog_users.profile = 2 THEN 'pisciner'
					WHEN watchdog_users.profile = 1 THEN 'staff'
					WHEN watchdog_users.profile = 4 THEN 'student'
					ELSE 'extern'
				END
			END,
			last_status_checked_at = excluded.last_status_checked_at,
			updated_at = excluded.updated_at
	`,
		login,
		boolToInt(isApprentice),
		detectedStatus,
		detectedStatus,
		checkedAtValue,
		now,
		now,
	)
	return err
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
	)

	err := storageQueryRow(`
		SELECT control_access_id, control_access_name, login_42, id_42, is_apprentice, profile,
			first_access, last_access, duration_seconds
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
	if settings, ok, err := loadUserSettings(user.Login42); err != nil {
		return User{}, false, err
	} else if ok {
		applyUserSettings(&user, settings)
	} else {
		user.Status42 = statusFromSignals(user.IsApprentice, user.Profile)
		user.Status = user.Status42
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
	)

	err := storageQueryRow(`
		SELECT control_access_id, control_access_name, login_42, id_42, is_apprentice, profile,
			first_access, last_access, duration_seconds
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
	if settings, ok, err := loadUserSettings(user.Login42); err != nil {
		return User{}, false, err
	} else if ok {
		applyUserSettings(&user, settings)
	} else {
		user.Status42 = statusFromSignals(user.IsApprentice, user.Profile)
		user.Status = user.Status42
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	Trace("CACHE", "badge events DB load for %s on %s: %d events", login, dayKey, len(events))
	return events, nil
}

func loadHistoricalUsersForDay(dayKey string, apprenticesOnly bool) ([]User, error) {
	if storageDB == nil || strings.TrimSpace(dayKey) == "" {
		return nil, nil
	}

	query := `
		SELECT control_access_id, control_access_name, login_42, id_42, is_apprentice, profile,
			first_access, last_access, badge_duration_seconds, retained_duration_seconds, day_type, day_type_label, required_attendance_hours, status, post_result, error_message
		FROM watchdog_daily_student_summaries
		WHERE day_key = ?
	`
	args := []any{dayKey}
	if apprenticesOnly {
		query += ` AND is_apprentice = 1`
	}
	query += ` ORDER BY login_42`

	rows, err := storageQuery(query, args...)
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
		var badgeDurationSeconds int64
		var durationSeconds int64
		var dayType sql.NullString
		var dayTypeLabel sql.NullString
		var requiredHours sql.NullFloat64
		var status sql.NullString
		var postResult sql.NullString
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
			&badgeDurationSeconds,
			&durationSeconds,
			&dayType,
			&dayTypeLabel,
			&requiredHours,
			&status,
			&postResult,
			&errorMessage,
		); err != nil {
			return nil, err
		}

		user.IsApprentice = isApprentice == 1
		user.Profile = ProfileType(profile)
		user.Duration = time.Duration(durationSeconds) * time.Second
		user.BadgeDuration = time.Duration(badgeDurationSeconds) * time.Second
		user.DayType = strings.TrimSpace(dayType.String)
		user.DayTypeLabel = strings.TrimSpace(dayTypeLabel.String)
		if requiredHours.Valid {
			value := requiredHours.Float64
			user.RequiredHours = &value
		}
		user.Status = status.String
		user.PostResult = postResult.String
		normalizeHistoricalSummaryStatus(&user)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}

	logins := make([]string, 0, len(users))
	for _, user := range users {
		logins = append(logins, user.Login42)
	}
	settingsByLogin, err := loadUserSettingsMap(logins)
	if err != nil {
		return nil, err
	}
	for index := range users {
		if settings, ok := settingsByLogin[normalizeLogin(users[index].Login42)]; ok {
			applyUserSettings(&users[index], settings)
		}
	}
	return users, nil
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

func loadDailyReportSummaries() ([]DailyReportSummary, error) {
	if storageDB == nil {
		return nil, nil
	}

	rows, err := storageQuery(`
		SELECT day_key
		FROM (
			SELECT DISTINCT day_key FROM watchdog_daily_student_summaries
			UNION
			SELECT DISTINCT day_key FROM watchdog_current_users
		) AS report_days
		ORDER BY day_key DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dayKeys := []string{}
	for rows.Next() {
		var dayKey string
		if err := rows.Scan(&dayKey); err != nil {
			return nil, err
		}
		dayKeys = append(dayKeys, dayKey)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	currentDayKey := currentRuntimeDayKey()
	reports := make([]DailyReportSummary, 0, len(dayKeys))
	for _, dayKey := range dayKeys {
		var (
			users []User
			err   error
		)
		if dayKey == currentDayKey {
			users, err = loadCurrentUsersForDay(dayKey, true)
		} else {
			users, err = loadHistoricalUsersForDay(dayKey, true)
		}
		if err != nil {
			return nil, err
		}
		report, err := buildDailyReportSummary(dayKey, users)
		if err != nil {
			return nil, err
		}
		report.Live = dayKey == currentDayKey
		if report.StudentCount == 0 {
			continue
		}
		reports = append(reports, report)
	}

	return reports, nil
}

func buildDailyReportSummary(dayKey string, users []User) (DailyReportSummary, error) {
	report := DailyReportSummary{DayKey: dayKey}
	postsByLogin, err := loadAttendancePostsForDay(dayKey)
	if err != nil {
		return report, err
	}
	isLiveDay := dayKey == currentRuntimeDayKey()

	for _, user := range users {
		if !user.IsApprentice {
			continue
		}
		posts := postsByLogin[normalizeLogin(user.Login42)]
		PopulateUserPostResult(&user, posts)

		report.StudentCount++
		if user.PostResult == POSTED || user.PostResult == POST_OFF {
			report.PostedCount++
			continue
		}
		if isLiveDay {
			continue
		}
		report.FailedCount++
	}

	return report, nil
}

func loadHistoricalLocationDaySummaries(login, monthKey string) (map[string]StudentAttendanceDaySummary, error) {
	if storageDB == nil {
		return nil, nil
	}

	login = normalizeLogin(login)
	monthPattern := monthKeyLikePattern(monthKey)
	fetchedRows, err := storageQuery(`
		SELECT day_key
		FROM watchdog_historical_location_fetches
		WHERE login_42 = ? AND day_key LIKE ?
	`, login, monthPattern)
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
		WHERE login_42 = ? AND day_key LIKE ?
		ORDER BY day_key, begin_at
	`, login, monthPattern)
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
	Trace("CACHE", "historical location summaries load for %s on %s: fetched_days=%d", login, monthKey, len(fetchedDays))

	return fetchedDays, nil
}

func loadHistoricalAttendanceBoundsByLogin(login, monthKey string) (map[string]*AttendanceBounds, error) {
	if storageDB == nil {
		return nil, nil
	}

	login = normalizeLogin(login)
	monthPattern := monthKeyLikePattern(monthKey)
	rows, err := storageQuery(`
		SELECT day_key, begin_at, end_at, source
		FROM watchdog_daily_attendance_bounds
		WHERE login_42 = ? AND day_key LIKE ?
	`, login, monthPattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	boundsByDay := make(map[string]*AttendanceBounds)
	for rows.Next() {
		var (
			dayKey     string
			beginAtRaw string
			endAtRaw   string
			source     string
		)
		if err := rows.Scan(&dayKey, &beginAtRaw, &endAtRaw, &source); err != nil {
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
		boundsByDay[dayKey] = &AttendanceBounds{
			BeginAt: beginAt,
			EndAt:   endAt,
			Source:  source,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	Trace("CACHE", "historical attendance bounds load for %s on %s: %d days", login, monthKey, len(boundsByDay))
	return boundsByDay, nil
}

func loadHistoricalAttendanceFetchedDays(login, monthKey string) (map[string]struct{}, error) {
	if storageDB == nil {
		return nil, nil
	}

	login = normalizeLogin(login)
	monthPattern := monthKeyLikePattern(monthKey)
	rows, err := storageQuery(`
		SELECT day_key
		FROM watchdog_historical_attendance_fetches
		WHERE login_42 = ? AND day_key LIKE ?
	`, login, monthPattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fetchedDays := make(map[string]struct{})
	for rows.Next() {
		var dayKey string
		if err := rows.Scan(&dayKey); err != nil {
			return nil, err
		}
		fetchedDays[dayKey] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	Trace("CACHE", "historical attendance fetched markers for %s on %s: %d days", login, monthKey, len(fetchedDays))
	return fetchedDays, nil
}

func ensureHistoricalMonthFetchWorkers() {
	historicalMonthFetchWorkersOnce.Do(func() {
		historicalMonthFetchQueue = make(chan historicalMonthFetchTask, historicalMonthFetchQueueSize)
		for i := 0; i < historicalMonthFetchWorkerCount; i++ {
			go func() {
				for task := range historicalMonthFetchQueue {
					processHistoricalMonthFetch(task.Login, task.MonthKey, task.DayKeys)
				}
			}()
		}
	})
}

func finishHistoricalMonthFetch(key string) {
	historicalMonthFetchesMu.Lock()
	delete(historicalMonthFetchesInFlight, key)
	historicalMonthFetchesMu.Unlock()
}

func processHistoricalMonthFetch(login, monthKey string, dayKeys []string) {
	key := login + "|" + monthKey
	defer finishHistoricalMonthFetch(key)
	Trace("API", "historical monthly fetch start for %s on %s: requested_day_count=%d", login, monthKey, len(dayKeys))

	locationDays, locationErr := fetchLocationSessionsForMonth(login, monthKey)
	if locationErr != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not fetch historical monthly locations for %s on %s: %v", login, monthKey, locationErr))
		return
	}

	attendanceDays, attendanceErr := fetchAttendanceBoundsForMonth(login, monthKey)
	if attendanceErr != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not fetch historical monthly attendances for %s on %s: %v", login, monthKey, attendanceErr))
	}
	Trace("BUILD", "historical monthly fetch payload for %s on %s: location_days=%d attendance_days=%d", login, monthKey, len(locationDays), len(attendanceDays))

	for _, dayKey := range dayKeys {
		sessions := locationDays[dayKey]
		if err := replaceHistoricalLocationSessions(dayKey, login, sessions); err != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not persist historical locations for %s on %s: %v", login, dayKey, err))
			continue
		}

		badgeEvents, err := loadBadgeEventsForLogin(dayKey, login)
		if err != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not inspect badge events for %s on %s: %v", login, dayKey, err))
			continue
		}
		if len(badgeEvents) == 0 && attendanceErr == nil {
			if err := replaceHistoricalAttendanceBounds(dayKey, login, attendanceDays[dayKey]); err != nil {
				Log(fmt.Sprintf("[WATCHDOG] WARNING: could not persist historical attendances for %s on %s: %v", login, dayKey, err))
				continue
			}
		} else if len(badgeEvents) > 0 {
			if err := replaceHistoricalAttendanceBounds(dayKey, login, nil); err != nil {
				Log(fmt.Sprintf("[WATCHDOG] WARNING: could not mark historical attendance fetch for %s on %s: %v", login, dayKey, err))
				continue
			}
		}
		Trace("CACHE", "historical monthly fetch stored for %s on %s: sessions=%d attendance_bounds=%t badge_events=%d", login, dayKey, len(sessions), attendanceDays[dayKey] != nil, len(badgeEvents))
	}
	NotifyLocationMonthUpdate(login, monthKey)
	Trace("API", "historical monthly fetch done for %s on %s", login, monthKey)
}

func enqueueHistoricalMonthFetch(login, monthKey string, dayKeys []string) {
	login = normalizeLogin(login)
	monthKey = strings.TrimSpace(monthKey)
	if storageDB == nil || login == "" || monthKey == "" || len(dayKeys) == 0 {
		return
	}
	ensureHistoricalMonthFetchWorkers()

	key := login + "|" + monthKey

	historicalMonthFetchesMu.Lock()
	if _, ok := historicalMonthFetchesInFlight[key]; ok {
		historicalMonthFetchesMu.Unlock()
		Trace("CACHE", "historical monthly fetch already running for %s on %s", login, monthKey)
		return
	}
	historicalMonthFetchesInFlight[key] = struct{}{}
	historicalMonthFetchesMu.Unlock()

	task := historicalMonthFetchTask{
		Login:    login,
		MonthKey: monthKey,
		DayKeys:  append([]string(nil), dayKeys...),
	}
	select {
	case historicalMonthFetchQueue <- task:
		Trace("CACHE", "historical monthly fetch enqueued for %s on %s: %d day(s)", login, monthKey, len(dayKeys))
	default:
		finishHistoricalMonthFetch(key)
		Log(fmt.Sprintf("[WATCHDOG] WARNING: skipped historical monthly fetch for %s on %s because the queue is full", login, monthKey))
	}
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

func monthKeyLikePattern(monthKey string) string {
	trimmed := strings.TrimSpace(monthKey)
	if trimmed == "" {
		return "%"
	}
	return trimmed + "-%"
}

func loadStudentAttendanceDays(login, monthKey string) ([]StudentAttendanceDaySummary, error) {
	if storageDB == nil {
		return nil, nil
	}

	login = normalizeLogin(login)
	trimmedMonthKey := strings.TrimSpace(monthKey)
	if trimmedMonthKey == "" {
		trimmedMonthKey = todayDayKeyInParis()[:7]
	}
	Trace("BUILD", "student attendance days load start for %s on %s", login, trimmedMonthKey)
	monthPattern := monthKeyLikePattern(trimmedMonthKey)
	rows, err := storageQuery(`
		SELECT day_key, first_access, last_access, retained_duration_seconds, status
		FROM watchdog_daily_student_summaries
		WHERE login_42 = ? AND day_key LIKE ?
		ORDER BY day_key DESC
	`, login, monthPattern)
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
	Trace("CACHE", "daily student summaries load for %s on %s: %d rows", login, trimmedMonthKey, len(dayMap))

	historicalLocationDays, err := loadHistoricalLocationDaySummaries(login, trimmedMonthKey)
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
	Trace("BUILD", "student attendance days after historical locations for %s on %s: %d days", login, trimmedMonthKey, len(dayMap))

	historicalAttendanceBoundsByDay, err := loadHistoricalAttendanceBoundsByLogin(login, trimmedMonthKey)
	if err != nil {
		return nil, err
	}
	for dayKey, bounds := range historicalAttendanceBoundsByDay {
		existing := dayMap[dayKey]
		if existing.DayKey == "" {
			existing.DayKey = dayKey
		}

		badgeEvents, err := loadBadgeEventsForLogin(dayKey, login)
		if err != nil {
			return nil, err
		}
		if len(badgeEvents) == 0 {
			sessions, err := loadHistoricalLocationSessions(dayKey, login)
			if err != nil {
				return nil, err
			}
			record, err := syntheticHistoricalStudentDay(login, dayKey, sessions, bounds)
			if err != nil {
				return nil, err
			}
			if !record.User.FirstAccess.IsZero() {
				existing.FirstAccess = record.User.FirstAccess
			}
			if !record.User.LastAccess.IsZero() {
				existing.LastAccess = record.User.LastAccess
			}
			existing.Duration = record.RetainedDuration
		} else {
			if existing.FirstAccess.IsZero() && bounds != nil {
				existing.FirstAccess = bounds.BeginAt
			}
			if existing.LastAccess.IsZero() && bounds != nil {
				existing.LastAccess = bounds.EndAt
			}
			if existing.Duration <= 0 {
				existing.Duration = attendanceBoundsDuration(bounds)
			}
		}
		dayMap[dayKey] = existing
	}
	Trace("BUILD", "student attendance days after attendance fallback merge for %s on %s: %d days", login, trimmedMonthKey, len(dayMap))

	historicalAttendanceFetchedDays, err := loadHistoricalAttendanceFetchedDays(login, trimmedMonthKey)
	if err != nil {
		return nil, err
	}

	currentDayKey := currentRuntimeDayKey()
	if strings.HasPrefix(currentDayKey, trimmedMonthKey+"-") {
		currentUser, ok, err := loadCurrentUserByLogin(currentDayKey, login)
		if err != nil {
			return nil, err
		}
		currentBadgeEvents, err := loadBadgeEventsForLogin(currentDayKey, login)
		if err != nil {
			return nil, err
		}
		currentLocationSessions, _ := SnapshotDailyLocationSessionsOrSchedule(login)
		currentAttendanceBounds, _ := SnapshotDailyAttendanceBoundsOrSchedule(login)
		effectiveCurrentBadgeEvents := applyAttendanceBoundsFallback(currentBadgeEvents, currentAttendanceBounds)

		currentDaySummary := StudentAttendanceDaySummary{
			DayKey:   currentDayKey,
			Duration: CombinedRetainedDuration(effectiveCurrentBadgeEvents, time.Time{}, time.Time{}, currentLocationSessions),
			Live:     true,
		}
		if ok {
			currentDaySummary.FirstAccess = currentUser.FirstAccess
			currentDaySummary.LastAccess = currentUser.LastAccess
			currentDaySummary.Status = currentUser.Status
			currentDaySummary.Duration = CombinedRetainedDuration(
				effectiveCurrentBadgeEvents,
				currentUser.FirstAccess,
				currentUser.LastAccess,
				currentLocationSessions,
			)
		}
		if len(effectiveCurrentBadgeEvents) > 0 {
			if currentDaySummary.FirstAccess.IsZero() {
				currentDaySummary.FirstAccess = effectiveCurrentBadgeEvents[0].Timestamp
			}
			if currentDaySummary.LastAccess.IsZero() {
				currentDaySummary.LastAccess = effectiveCurrentBadgeEvents[len(effectiveCurrentBadgeEvents)-1].Timestamp
			}
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
		Trace("BUILD", "student attendance current day merged for %s on %s: badge_events=%d location_sessions=%d attendance_bounds=%t", login, currentDayKey, len(currentBadgeEvents), len(currentLocationSessions), currentAttendanceBounds != nil)
	}
	if trimmedMonthKey != "" {
		dayKeys, err := monthDayKeys(trimmedMonthKey)
		if err != nil {
			return nil, err
		}
		todayKey := todayDayKeyInParis()
		missingHistoricalDayKeys := make([]string, 0, len(dayKeys))
		for _, dayKey := range dayKeys {
			summary, ok := dayMap[dayKey]
			if !ok {
				summary = StudentAttendanceDaySummary{
					DayKey:   dayKey,
					Duration: 0,
				}
			}
			if dayKey < todayKey {
				if _, fetched := historicalAttendanceFetchedDays[dayKey]; !fetched {
					summary.Loading = true
					missingHistoricalDayKeys = append(missingHistoricalDayKeys, dayKey)
				}
			}
			dayMap[dayKey] = summary
		}
		Trace("BUILD", "student attendance missing historical days for %s on %s: %d day(s) scheduled", login, trimmedMonthKey, len(missingHistoricalDayKeys))
		enqueueHistoricalMonthFetch(login, trimmedMonthKey, missingHistoricalDayKeys)

		calendarByDay, err := loadStudentCalendarMonth(login, trimmedMonthKey)
		if err != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not load CFA calendar for %s on %s: %v", login, trimmedMonthKey, err))
			calendarByDay, err = buildDefaultStudentCalendarMonth(trimmedMonthKey)
			if err != nil {
				return nil, err
			}
		}
		for dayKey, calendarDay := range calendarByDay {
			summary, ok := dayMap[dayKey]
			if !ok {
				summary = StudentAttendanceDaySummary{DayKey: dayKey}
			}
			summary.DayType = calendarDay.DayType
			summary.DayTypeLabel = calendarDay.DayTypeLabel
			summary.RequiredAttendanceHours = calendarDay.RequiredAttendanceHours
			dayMap[dayKey] = summary
		}
		Trace("BUILD", "student attendance CFA merge for %s on %s: %d calendar days", login, trimmedMonthKey, len(calendarByDay))
	}

	allowedDayKeys := map[string]struct{}{}
	if trimmedMonthKey != "" {
		dayKeys, err := monthDayKeys(trimmedMonthKey)
		if err != nil {
			return nil, err
		}
		allowedDayKeys = make(map[string]struct{}, len(dayKeys))
		for _, dayKey := range dayKeys {
			allowedDayKeys[dayKey] = struct{}{}
		}
	}

	days := make([]StudentAttendanceDaySummary, 0, len(dayMap))
	for _, day := range dayMap {
		if len(allowedDayKeys) > 0 {
			if _, ok := allowedDayKeys[day.DayKey]; !ok {
				continue
			}
		}
		days = append(days, day)
	}
	sort.Slice(days, func(i, j int) bool {
		return days[i].DayKey > days[j].DayKey
	})
	Trace("BUILD", "student attendance days load done for %s on %s: %d returned days", login, trimmedMonthKey, len(days))

	return days, nil
}

func loadLatestHistoricalAdminUsers() ([]AdminUserSummary, error) {
	if storageDB == nil {
		return nil, nil
	}

	rows, err := storageQuery(`
		SELECT DISTINCT ON (summaries.login_42)
			summaries.login_42,
			summaries.is_apprentice,
			summaries.profile,
			COALESCE(users.status, ''),
			COALESCE(users.status_42, ''),
			COALESCE(users.status_overridden, 0),
			COALESCE(summaries.last_access, summaries.first_access),
			summaries.retained_duration_seconds,
			COALESCE(users.is_blacklisted, 0),
			COALESCE(users.badge_posting_off, 0),
			COALESCE(users.blacklist_reason, '')
		FROM watchdog_daily_student_summaries AS summaries
		LEFT JOIN watchdog_users AS users
			ON users.login_42 = summaries.login_42
		ORDER BY summaries.login_42, COALESCE(summaries.last_access, summaries.first_access) DESC NULLS LAST, summaries.day_key DESC
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
		var statusOverridden int
		var lastBadgeAt sql.NullString
		var durationSeconds int64
		var isBlacklisted int
		var badgePostingOff int
		if err := rows.Scan(&user.Login42, &isApprentice, &profile, &user.Status, &user.Status42, &statusOverridden, &lastBadgeAt, &durationSeconds, &isBlacklisted, &badgePostingOff, &user.BlacklistReason); err != nil {
			return nil, err
		}
		user.IsApprentice = isApprentice == 1
		user.Profile = ProfileType(profile)
		user.Status = coalesceManagedStatus(user.Status, statusFromSignals(user.IsApprentice, user.Profile))
		user.Status42 = coalesceManagedStatus(user.Status42, statusFromSignals(user.IsApprentice, user.Profile))
		user.StatusOverridden = statusOverridden == 1
		user.IsBlacklisted = isBlacklisted == 1
		user.BadgePostingOff = badgePostingOff == 1
		user.DayDuration = time.Duration(durationSeconds) * time.Second
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

func loadLatestHistoricalAdminUserByLogin(login string) (AdminUserSummary, bool, error) {
	if storageDB == nil {
		return AdminUserSummary{}, false, nil
	}

	login = normalizeLogin(login)
	var user AdminUserSummary
	var isApprentice int
	var profile int
	var statusOverridden int
	var lastBadgeAt sql.NullString
	var durationSeconds int64
	var isBlacklisted int
	var badgePostingOff int

	err := storageQueryRow(`
		SELECT
			summaries.login_42,
			summaries.is_apprentice,
			summaries.profile,
			COALESCE(users.status, ''),
			COALESCE(users.status_42, ''),
			COALESCE(users.status_overridden, 0),
			COALESCE(summaries.last_access, summaries.first_access),
			summaries.retained_duration_seconds,
			COALESCE(users.is_blacklisted, 0),
			COALESCE(users.badge_posting_off, 0),
			COALESCE(users.blacklist_reason, '')
		FROM watchdog_daily_student_summaries AS summaries
		LEFT JOIN watchdog_users AS users
			ON users.login_42 = summaries.login_42
		WHERE summaries.login_42 = ?
		ORDER BY COALESCE(summaries.last_access, summaries.first_access) DESC NULLS LAST, summaries.day_key DESC
		LIMIT 1
	`, login).Scan(&user.Login42, &isApprentice, &profile, &user.Status, &user.Status42, &statusOverridden, &lastBadgeAt, &durationSeconds, &isBlacklisted, &badgePostingOff, &user.BlacklistReason)
	if err == sql.ErrNoRows {
		return AdminUserSummary{}, false, nil
	}
	if err != nil {
		return AdminUserSummary{}, false, err
	}

	user.IsApprentice = isApprentice == 1
	user.Profile = ProfileType(profile)
	user.Status = coalesceManagedStatus(user.Status, statusFromSignals(user.IsApprentice, user.Profile))
	user.Status42 = coalesceManagedStatus(user.Status42, statusFromSignals(user.IsApprentice, user.Profile))
	user.StatusOverridden = statusOverridden == 1
	user.IsBlacklisted = isBlacklisted == 1
	user.BadgePostingOff = badgePostingOff == 1
	user.DayDuration = time.Duration(durationSeconds) * time.Second
	if lastBadgeAt.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, lastBadgeAt.String)
		if parseErr != nil {
			return AdminUserSummary{}, false, parseErr
		}
		user.LastBadgeAt = parsed
	}

	return user, true, nil
}

func loadCurrentAdminUsers() ([]AdminUserSummary, error) {
	if storageDB == nil {
		return nil, nil
	}

	dayKey := currentRuntimeDayKey()
	rows, err := storageQuery(`
		SELECT DISTINCT ON (current_users.login_42)
			current_users.login_42,
			current_users.is_apprentice,
			current_users.profile,
			COALESCE(users.status, ''),
			COALESCE(users.status_42, ''),
			COALESCE(users.status_overridden, 0),
			current_users.first_access,
			COALESCE(current_users.last_access, current_users.first_access),
			current_users.duration_seconds,
			COALESCE(users.is_blacklisted, 0),
			COALESCE(users.badge_posting_off, 0),
			COALESCE(users.blacklist_reason, '')
		FROM watchdog_current_users AS current_users
		LEFT JOIN watchdog_users AS users
			ON users.login_42 = current_users.login_42
		WHERE current_users.day_key = ? AND COALESCE(current_users.last_access, current_users.first_access) IS NOT NULL
		ORDER BY current_users.login_42, COALESCE(current_users.last_access, current_users.first_access) DESC NULLS LAST, current_users.updated_at DESC
	`, dayKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []AdminUserSummary
	for rows.Next() {
		var user AdminUserSummary
		var isApprentice int
		var profile int
		var statusOverridden int
		var firstAccess sql.NullString
		var lastBadgeAt sql.NullString
		var durationSeconds int64
		var isBlacklisted int
		var badgePostingOff int
		if err := rows.Scan(&user.Login42, &isApprentice, &profile, &user.Status, &user.Status42, &statusOverridden, &firstAccess, &lastBadgeAt, &durationSeconds, &isBlacklisted, &badgePostingOff, &user.BlacklistReason); err != nil {
			return nil, err
		}
		user.IsApprentice = isApprentice == 1
		user.Profile = ProfileType(profile)
		user.Status = coalesceManagedStatus(user.Status, statusFromSignals(user.IsApprentice, user.Profile))
		user.Status42 = coalesceManagedStatus(user.Status42, statusFromSignals(user.IsApprentice, user.Profile))
		user.StatusOverridden = statusOverridden == 1
		user.IsBlacklisted = isBlacklisted == 1
		user.BadgePostingOff = badgePostingOff == 1
		storedDuration := time.Duration(durationSeconds) * time.Second
		user.DayDuration = storedDuration

		var firstAccessTime time.Time
		if firstAccess.Valid {
			firstAccessTime, err = time.Parse(time.RFC3339Nano, firstAccess.String)
			if err != nil {
				return nil, err
			}
		}
		if lastBadgeAt.Valid {
			user.LastBadgeAt, err = time.Parse(time.RFC3339Nano, lastBadgeAt.String)
			if err != nil {
				return nil, err
			}
		}

		badgeEvents, err := loadBadgeEventsForLogin(dayKey, user.Login42)
		if err != nil {
			return nil, err
		}
		locationSessions, _ := SnapshotDailyLocationSessionsOrSchedule(user.Login42)
		attendanceBounds, _ := SnapshotDailyAttendanceBoundsOrSchedule(user.Login42)
		effectiveBadgeEvents := applyAttendanceBoundsFallback(badgeEvents, attendanceBounds)
		recalculatedDuration := CombinedRetainedDuration(effectiveBadgeEvents, firstAccessTime, user.LastBadgeAt, locationSessions)
		if recalculatedDuration > user.DayDuration {
			user.DayDuration = recalculatedDuration
		}
		if user.LastBadgeAt.IsZero() && len(effectiveBadgeEvents) > 0 {
			user.LastBadgeAt = effectiveBadgeEvents[len(effectiveBadgeEvents)-1].Timestamp
		}

		users = append(users, user)
	}
	return users, rows.Err()
}

func loadCurrentAdminUserByLogin(login string) (AdminUserSummary, bool, error) {
	if storageDB == nil {
		return AdminUserSummary{}, false, nil
	}

	login = normalizeLogin(login)
	dayKey := currentRuntimeDayKey()

	var user AdminUserSummary
	var isApprentice int
	var profile int
	var statusOverridden int
	var firstAccess sql.NullString
	var lastBadgeAt sql.NullString
	var durationSeconds int64
	var isBlacklisted int
	var badgePostingOff int

	err := storageQueryRow(`
		SELECT
			current_users.login_42,
			current_users.is_apprentice,
			current_users.profile,
			COALESCE(users.status, ''),
			COALESCE(users.status_42, ''),
			COALESCE(users.status_overridden, 0),
			current_users.first_access,
			COALESCE(current_users.last_access, current_users.first_access),
			current_users.duration_seconds,
			COALESCE(users.is_blacklisted, 0),
			COALESCE(users.badge_posting_off, 0),
			COALESCE(users.blacklist_reason, '')
		FROM watchdog_current_users AS current_users
		LEFT JOIN watchdog_users AS users
			ON users.login_42 = current_users.login_42
		WHERE current_users.day_key = ?
			AND current_users.login_42 = ?
			AND COALESCE(current_users.last_access, current_users.first_access) IS NOT NULL
		ORDER BY COALESCE(current_users.last_access, current_users.first_access) DESC NULLS LAST, current_users.updated_at DESC
		LIMIT 1
	`, dayKey, login).Scan(&user.Login42, &isApprentice, &profile, &user.Status, &user.Status42, &statusOverridden, &firstAccess, &lastBadgeAt, &durationSeconds, &isBlacklisted, &badgePostingOff, &user.BlacklistReason)
	if err == sql.ErrNoRows {
		return AdminUserSummary{}, false, nil
	}
	if err != nil {
		return AdminUserSummary{}, false, err
	}

	user.IsApprentice = isApprentice == 1
	user.Profile = ProfileType(profile)
	user.Status = coalesceManagedStatus(user.Status, statusFromSignals(user.IsApprentice, user.Profile))
	user.Status42 = coalesceManagedStatus(user.Status42, statusFromSignals(user.IsApprentice, user.Profile))
	user.StatusOverridden = statusOverridden == 1
	user.IsBlacklisted = isBlacklisted == 1
	user.BadgePostingOff = badgePostingOff == 1
	user.DayDuration = time.Duration(durationSeconds) * time.Second

	var firstAccessTime time.Time
	if firstAccess.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, firstAccess.String)
		if parseErr != nil {
			return AdminUserSummary{}, false, parseErr
		}
		firstAccessTime = parsed
	}
	if lastBadgeAt.Valid {
		parsed, parseErr := time.Parse(time.RFC3339Nano, lastBadgeAt.String)
		if parseErr != nil {
			return AdminUserSummary{}, false, parseErr
		}
		user.LastBadgeAt = parsed
	}

	badgeEvents, err := loadBadgeEventsForLogin(dayKey, user.Login42)
	if err != nil {
		return AdminUserSummary{}, false, err
	}
	locationSessions, _ := SnapshotDailyLocationSessionsOrSchedule(user.Login42)
	attendanceBounds, _ := SnapshotDailyAttendanceBoundsOrSchedule(user.Login42)
	effectiveBadgeEvents := applyAttendanceBoundsFallback(badgeEvents, attendanceBounds)
	recalculatedDuration := CombinedRetainedDuration(effectiveBadgeEvents, firstAccessTime, user.LastBadgeAt, locationSessions)
	if recalculatedDuration > user.DayDuration {
		user.DayDuration = recalculatedDuration
	}
	if user.LastBadgeAt.IsZero() && len(effectiveBadgeEvents) > 0 {
		user.LastBadgeAt = effectiveBadgeEvents[len(effectiveBadgeEvents)-1].Timestamp
	}

	return user, true, nil
}

func loadCurrentAdminDayProfiles() ([]AdminUserSummary, error) {
	if storageDB == nil {
		return nil, nil
	}

	rows, err := storageQuery(`
		SELECT
			profiles.login_42,
			profiles.is_apprentice,
			profiles.profile,
			COALESCE(users.status, ''),
			COALESCE(users.status_42, ''),
			COALESCE(users.status_overridden, 0),
			COALESCE(users.is_blacklisted, 0),
			COALESCE(users.badge_posting_off, 0),
			COALESCE(users.blacklist_reason, '')
		FROM watchdog_day_profiles AS profiles
		LEFT JOIN watchdog_users AS users
			ON users.login_42 = profiles.login_42
		WHERE profiles.day_key = ?
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
		var statusOverridden int
		var isBlacklisted int
		var badgePostingOff int
		if err := rows.Scan(&user.Login42, &isApprentice, &profile, &user.Status, &user.Status42, &statusOverridden, &isBlacklisted, &badgePostingOff, &user.BlacklistReason); err != nil {
			return nil, err
		}
		user.IsApprentice = isApprentice == 1
		user.Profile = ProfileType(profile)
		user.Status = coalesceManagedStatus(user.Status, statusFromSignals(user.IsApprentice, user.Profile))
		user.Status42 = coalesceManagedStatus(user.Status42, statusFromSignals(user.IsApprentice, user.Profile))
		user.StatusOverridden = statusOverridden == 1
		user.IsBlacklisted = isBlacklisted == 1
		user.BadgePostingOff = badgePostingOff == 1
		users = append(users, user)
	}
	return users, rows.Err()
}

func loadCurrentAdminDayProfileByLogin(login string) (AdminUserSummary, bool, error) {
	if storageDB == nil {
		return AdminUserSummary{}, false, nil
	}

	login = normalizeLogin(login)
	var user AdminUserSummary
	var isApprentice int
	var profile int
	var statusOverridden int
	var isBlacklisted int
	var badgePostingOff int

	err := storageQueryRow(`
		SELECT
			profiles.login_42,
			profiles.is_apprentice,
			profiles.profile,
			COALESCE(users.status, ''),
			COALESCE(users.status_42, ''),
			COALESCE(users.status_overridden, 0),
			COALESCE(users.is_blacklisted, 0),
			COALESCE(users.badge_posting_off, 0),
			COALESCE(users.blacklist_reason, '')
		FROM watchdog_day_profiles AS profiles
		LEFT JOIN watchdog_users AS users
			ON users.login_42 = profiles.login_42
		WHERE profiles.day_key = ? AND profiles.login_42 = ?
		LIMIT 1
	`, currentRuntimeDayKey(), login).Scan(&user.Login42, &isApprentice, &profile, &user.Status, &user.Status42, &statusOverridden, &isBlacklisted, &badgePostingOff, &user.BlacklistReason)
	if err == sql.ErrNoRows {
		return AdminUserSummary{}, false, nil
	}
	if err != nil {
		return AdminUserSummary{}, false, err
	}

	user.IsApprentice = isApprentice == 1
	user.Profile = ProfileType(profile)
	user.Status = coalesceManagedStatus(user.Status, statusFromSignals(user.IsApprentice, user.Profile))
	user.Status42 = coalesceManagedStatus(user.Status42, statusFromSignals(user.IsApprentice, user.Profile))
	user.StatusOverridden = statusOverridden == 1
	user.IsBlacklisted = isBlacklisted == 1
	user.BadgePostingOff = badgePostingOff == 1

	return user, true, nil
}

func loadBaseApprenticeUsers() (map[string]User, error) {
	if storageDB == nil {
		return map[string]User{}, nil
	}

	rows, err := storageQuery(`
		SELECT login_42, is_apprentice, profile, status, status_42, status_overridden,
			is_blacklisted, badge_posting_off, blacklist_reason
		FROM watchdog_users
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make(map[string]User)
	for rows.Next() {
		var user User
		var isApprentice int
		var profile int
		var statusOverridden int
		var isBlacklisted int
		var badgePostingOff int

		if err := rows.Scan(
			&user.Login42,
			&isApprentice,
			&profile,
			&user.Status,
			&user.Status42,
			&statusOverridden,
			&isBlacklisted,
			&badgePostingOff,
			&user.BlacklistReason,
		); err != nil {
			return nil, err
		}

		user.Login42 = normalizeLogin(user.Login42)
		user.IsApprentice = isApprentice == 1
		user.Profile = ProfileType(profile)
		user.Status = coalesceManagedStatus(user.Status, statusFromSignals(user.IsApprentice, user.Profile))
		user.Status42 = coalesceManagedStatus(user.Status42, statusFromSignals(user.IsApprentice, user.Profile))
		user.StatusOverridden = statusOverridden == 1
		user.IsBlacklisted = isBlacklisted == 1
		user.BadgePostingOff = badgePostingOff == 1
		user.BlacklistReason = strings.TrimSpace(user.BlacklistReason)

		effectiveStatus := user.Status42
		if user.StatusOverridden {
			effectiveStatus = coalesceManagedStatus(user.Status, user.Status42)
		}
		if normalizeManagedUserStatus(effectiveStatus) != "apprentice" {
			continue
		}

		user.IsApprentice, user.Profile = signalsFromStatus(effectiveStatus)
		user.Status = effectiveStatus
		user.Status42 = coalesceManagedStatus(user.Status42, effectiveStatus)
		users[user.Login42] = user
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
		FROM watchdog_daily_attendance_bounds
		WHERE day_key = ?
		UNION
		SELECT DISTINCT login_42
		FROM watchdog_badge_events
		WHERE day_key = ?
	`
	args := []any{dayKey, dayKey, dayKey, dayKey}

	if dayKey == currentRuntimeDayKey() {
		query = `
			SELECT DISTINCT login_42
			FROM watchdog_current_users
			WHERE day_key = ? AND COALESCE(first_access, last_access) IS NOT NULL
			UNION
			SELECT DISTINCT login_42
			FROM watchdog_day_profiles
			WHERE day_key = ?
			UNION
			SELECT DISTINCT login_42
			FROM watchdog_badge_events
			WHERE day_key = ?
		`
		args = []any{dayKey, dayKey, dayKey}
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
	currentProfiles, err := loadCurrentAdminDayProfiles()
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

	usersByLogin := make(map[string]AdminUserSummary, len(historicalUsers)+len(currentUsers)+len(currentProfiles))
	for _, user := range historicalUsers {
		usersByLogin[normalizeLogin(user.Login42)] = user
	}
	for _, user := range currentProfiles {
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
		status := coalesceManagedStatus(user.Status, statusFromSignals(user.IsApprentice, user.Profile))
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	Trace("CACHE", "historical locations DB load for %s on %s: %d sessions", login, dayKey, len(sessions))
	return sessions, nil
}

func loadHistoricalAttendanceBounds(dayKey, login string) (*AttendanceBounds, error) {
	if storageDB == nil {
		return nil, nil
	}

	login = strings.ToLower(strings.TrimSpace(login))
	var (
		beginAtRaw string
		endAtRaw   string
		source     string
	)
	err := storageQueryRow(`
		SELECT begin_at, end_at, source
		FROM watchdog_daily_attendance_bounds
		WHERE day_key = ? AND login_42 = ?
	`, dayKey, login).Scan(&beginAtRaw, &endAtRaw, &source)
	if err == sql.ErrNoRows {
		Trace("CACHE", "historical attendance bounds DB miss for %s on %s", login, dayKey)
		return nil, nil
	}
	if err != nil {
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
	bounds := &AttendanceBounds{
		BeginAt: beginAt,
		EndAt:   endAt,
		Source:  source,
	}
	Trace("CACHE", "historical attendance bounds DB load for %s on %s: %s source=%s", login, dayKey, traceBounds(bounds.BeginAt, bounds.EndAt), bounds.Source)
	return bounds, nil
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

func historicalAttendanceBoundsFetched(dayKey, login string) (bool, error) {
	if storageDB == nil {
		return false, nil
	}
	login = strings.ToLower(strings.TrimSpace(login))

	var fetchedAt string
	err := storageQueryRow(`
		SELECT fetched_at
		FROM watchdog_historical_attendance_fetches
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

	if err = tx.Commit(); err != nil {
		return err
	}
	Trace("CACHE", "historical locations DB store for %s on %s: %d sessions", login, dayKey, len(sessions))
	return nil
}

func replaceHistoricalAttendanceBounds(dayKey, login string, bounds *AttendanceBounds) (err error) {
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

	if _, err = tx.Exec(rebindPostgres(`DELETE FROM watchdog_daily_attendance_bounds WHERE day_key = ? AND login_42 = ?`), dayKey, login); err != nil {
		return err
	}

	if bounds != nil {
		if _, err = tx.Exec(rebindPostgres(`
			INSERT INTO watchdog_daily_attendance_bounds (
				day_key, login_42, begin_at, end_at, source, created_at
			) VALUES (?, ?, ?, ?, ?, ?)
		`),
			dayKey,
			login,
			bounds.BeginAt.UTC().Format(time.RFC3339Nano),
			bounds.EndAt.UTC().Format(time.RFC3339Nano),
			bounds.Source,
			time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			return err
		}
	}

	if _, err = tx.Exec(rebindPostgres(`
		INSERT INTO watchdog_historical_attendance_fetches (day_key, login_42, fetched_at)
		VALUES (?, ?, ?)
		ON CONFLICT(day_key, login_42) DO UPDATE SET fetched_at = excluded.fetched_at
	`), dayKey, login, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	Trace("CACHE", "historical attendance bounds DB store for %s on %s: present=%t", login, dayKey, bounds != nil)
	return nil
}

func syntheticHistoricalStudentDay(login, dayKey string, sessions []LocationSession, attendanceBounds *AttendanceBounds) (HistoricalStudentDay, error) {
	login = normalizeLogin(login)
	badgeEvents := attendanceBoundsAsBadgeEvents(attendanceBounds)
	record := HistoricalStudentDay{
		DayKey:           dayKey,
		User:             User{Login42: login, Profile: Student},
		BadgeEvents:      badgeEvents,
		LocationSessions: sessions,
		AttendancePosts:  []AttendancePostRecord{},
		RetainedDuration: CombinedRetainedDuration(badgeEvents, time.Time{}, time.Time{}, sessions),
		BadgeDuration:    0,
	}
	if attendanceBounds != nil {
		record.User.FirstAccess = attendanceBounds.BeginAt
		record.User.LastAccess = attendanceBounds.EndAt
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
	if err := saveUserIdentity(record.User); err != nil {
		return err
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
			first_access, last_access, badge_duration_seconds, retained_duration_seconds, day_type, day_type_label, required_attendance_hours, status, post_result, error_message, finalized_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			day_type = excluded.day_type,
			day_type_label = excluded.day_type_label,
			required_attendance_hours = excluded.required_attendance_hours,
			status = excluded.status,
			post_result = excluded.post_result,
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
		strings.TrimSpace(record.CalendarDay.DayType),
		strings.TrimSpace(record.CalendarDay.DayTypeLabel),
		record.CalendarDay.RequiredAttendanceHours,
		record.User.Status,
		record.User.PostResult,
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
		dayType         sql.NullString
		dayTypeLabel    sql.NullString
		requiredHours   sql.NullFloat64
		status          sql.NullString
		postResult      sql.NullString
		errorMessage    sql.NullString
	)

	err := storageQueryRow(`
		SELECT control_access_id, control_access_name, id_42, is_apprentice, profile,
			first_access, last_access, badge_duration_seconds, retained_duration_seconds, day_type, day_type_label, required_attendance_hours, status, post_result, error_message
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
		&dayType,
		&dayTypeLabel,
		&requiredHours,
		&status,
		&postResult,
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
	record.User.PostResult = postResult.String
	normalizeHistoricalSummaryStatus(&record.User)
	record.BadgeDuration = time.Duration(badgeSeconds) * time.Second
	record.RetainedDuration = time.Duration(retainedSeconds) * time.Second
	record.CalendarDay.DayType = strings.TrimSpace(dayType.String)
	record.CalendarDay.DayTypeLabel = strings.TrimSpace(dayTypeLabel.String)
	if requiredHours.Valid {
		value := requiredHours.Float64
		record.CalendarDay.RequiredAttendanceHours = &value
	}
	record.User.BadgeDuration = record.BadgeDuration
	record.User.DayType = record.CalendarDay.DayType
	record.User.DayTypeLabel = record.CalendarDay.DayTypeLabel
	record.User.RequiredHours = record.CalendarDay.RequiredAttendanceHours

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
	realBadgeEvents := badgeEventsByLogin[login]
	record.BadgeEvents = realBadgeEvents

	attendanceBounds, err := loadHistoricalAttendanceBounds(dayKey, login)
	if err != nil {
		return HistoricalStudentDay{}, false, err
	}

	record.LocationSessions, err = loadHistoricalLocationSessions(dayKey, login)
	if err != nil {
		return HistoricalStudentDay{}, false, err
	}
	if len(realBadgeEvents) == 0 {
		record.BadgeEvents = applyAttendanceBoundsFallback(nil, attendanceBounds)
		if record.User.FirstAccess.IsZero() && attendanceBounds != nil {
			record.User.FirstAccess = attendanceBounds.BeginAt
		}
		if record.User.LastAccess.IsZero() && attendanceBounds != nil {
			record.User.LastAccess = attendanceBounds.EndAt
		}
		record.RetainedDuration = CombinedRetainedDuration(record.BadgeEvents, record.User.FirstAccess, record.User.LastAccess, record.LocationSessions)
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

func loadAttendancePostsForDay(dayKey string) (map[string][]AttendancePostRecord, error) {
	if storageDB == nil || strings.TrimSpace(dayKey) == "" {
		return map[string][]AttendancePostRecord{}, nil
	}

	rows, err := storageQuery(`
		SELECT login_42, id_42, control_access_id, begin_at, end_at, payload_json, http_status,
			response_status, error_message, success, created_at
		FROM watchdog_attendance_posts
		WHERE day_key = ?
		ORDER BY login_42, created_at
	`, dayKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]AttendancePostRecord)
	for rows.Next() {
		var record AttendancePostRecord
		var login string
		var beginAtRaw sql.NullString
		var endAtRaw sql.NullString
		var payloadRaw string
		var httpStatus sql.NullInt64
		var responseStatus sql.NullString
		var errorMessage sql.NullString
		var success int
		var createdAtRaw string

		if err := rows.Scan(
			&login,
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
		record.Login42 = normalizeLogin(login)
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

		out[record.Login42] = append(out[record.Login42], record)
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
	currentUsers, err := loadCurrentUsersForDay(dayKey, false)
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
			user = User{
				Login42:  login,
				Profile:  Student,
				Status:   "student",
				Status42: "student",
			}
			if summary, summaryOK, summaryErr := AdminUserByLogin(login); summaryErr != nil {
				return summaryErr
			} else if summaryOK {
				user.IsApprentice = summary.IsApprentice
				user.Profile = summary.Profile
				user.Status = summary.Status
				user.Status42 = summary.Status42
				user.StatusOverridden = summary.StatusOverridden
				user.IsBlacklisted = summary.IsBlacklisted
				user.BadgePostingOff = summary.BadgePostingOff
				user.BlacklistReason = summary.BlacklistReason
			}
		}
		user.Login42 = login
		attendancePosts, err := loadAttendancePosts(dayKey, login)
		if err != nil {
			return err
		}

		realBadgeEvents := badgeEventsByLogin[login]
		locationSessions, attendanceBounds, locationErr, attendanceErr := fetchSupplementalDayData(login, dayKey, true, len(realBadgeEvents) == 0)
		if locationErr != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not fetch historical locations for %s on %s: %v", login, dayKey, locationErr))
			locationSessions = nil
		}
		if err := replaceHistoricalLocationSessions(dayKey, login, locationSessions); err != nil {
			return err
		}
		if len(realBadgeEvents) == 0 {
			if attendanceErr != nil {
				Log(fmt.Sprintf("[WATCHDOG] WARNING: could not fetch Chronos attendance fallback for %s on %s: %v", login, dayKey, attendanceErr))
			} else if err := replaceHistoricalAttendanceBounds(dayKey, login, attendanceBounds); err != nil {
				return err
			}
			if user.FirstAccess.IsZero() && attendanceBounds != nil {
				user.FirstAccess = attendanceBounds.BeginAt
			}
			if user.LastAccess.IsZero() && attendanceBounds != nil {
				user.LastAccess = attendanceBounds.EndAt
			}
		}

		effectiveBadgeEvents := applyAttendanceBoundsFallback(realBadgeEvents, attendanceBounds)
		if user.FirstAccess.IsZero() && len(effectiveBadgeEvents) > 0 {
			user.FirstAccess = effectiveBadgeEvents[0].Timestamp
		}
		if user.LastAccess.IsZero() && len(effectiveBadgeEvents) > 0 {
			user.LastAccess = effectiveBadgeEvents[len(effectiveBadgeEvents)-1].Timestamp
		}
		user.Duration = CombinedRetainedDuration(effectiveBadgeEvents, user.FirstAccess, user.LastAccess, nil)
		PopulateUserPostResult(&user, attendancePosts)
		calendarDay, calendarErr := loadStudentCalendarDay(login, dayKey)
		if calendarErr != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not load school day calendar for %s on %s while finalizing report: %v", login, dayKey, calendarErr))
		}

		record := HistoricalStudentDay{
			DayKey:           dayKey,
			User:             user,
			CalendarDay:      calendarDay,
			BadgeEvents:      effectiveBadgeEvents,
			LocationSessions: locationSessions,
			RetainedDuration: CombinedRetainedDuration(effectiveBadgeEvents, user.FirstAccess, user.LastAccess, locationSessions),
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
	Trace("BUILD", "historical student day load start for %s on %s", login, dayKey)
	record, ok, err := loadHistoricalStudentDay(login, dayKey)
	if err != nil || ok {
		if err != nil {
			Trace("BUILD", "historical student day load aborted for %s on %s: %v", login, dayKey, err)
		} else {
			Trace("CACHE", "historical student day summary hit for %s on %s", login, dayKey)
		}
		return record, ok, err
	}

	badgeEvents, err := loadBadgeEventsForLogin(dayKey, login)
	if err != nil {
		return HistoricalStudentDay{}, false, err
	}

	cachedSessions, err := loadHistoricalLocationSessions(dayKey, login)
	if err != nil {
		return HistoricalStudentDay{}, false, err
	}
	cachedAttendanceBounds, err := loadHistoricalAttendanceBounds(dayKey, login)
	if err != nil {
		return HistoricalStudentDay{}, false, err
	}

	locationFetched, err := historicalLocationSessionsFetched(dayKey, login)
	if err != nil {
		return HistoricalStudentDay{}, false, err
	}
	attendanceFetched, err := historicalAttendanceBoundsFetched(dayKey, login)
	if err != nil {
		return HistoricalStudentDay{}, false, err
	}

	if locationFetched && (attendanceFetched || len(badgeEvents) > 0) {
		if len(badgeEvents) > 0 && !attendanceFetched {
			if err := replaceHistoricalAttendanceBounds(dayKey, login, nil); err != nil {
				return HistoricalStudentDay{}, false, err
			}
			attendanceFetched = true
		}
		record, err := syntheticHistoricalStudentDay(login, dayKey, cachedSessions, cachedAttendanceBounds)
		if err != nil {
			return HistoricalStudentDay{}, false, err
		}
		Trace("CACHE", "historical student day synthesized fully from cache for %s on %s: sessions=%d attendance_bounds=%t badge_events=%d", login, dayKey, len(cachedSessions), cachedAttendanceBounds != nil, len(badgeEvents))
		return record, true, nil
	}

	needLocations := !locationFetched
	needAttendance := !attendanceFetched && len(badgeEvents) == 0
	Trace("API", "historical student day fetch decision for %s on %s: needLocations=%t needAttendance=%t", login, dayKey, needLocations, needAttendance)
	locationSessions, attendanceBounds, locationErr, attendanceErr := fetchSupplementalDayData(login, dayKey, needLocations, needAttendance)
	if locationErr != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not fetch historical locations for %s on %s: %v", login, dayKey, locationErr))
		record, synthErr := syntheticHistoricalStudentDay(login, dayKey, cachedSessions, cachedAttendanceBounds)
		if synthErr != nil {
			return HistoricalStudentDay{}, false, synthErr
		}
		Trace("CACHE", "historical student day fallback to stale cache for %s on %s after location fetch error", login, dayKey)
		return record, true, nil
	}
	if needLocations {
		if err := replaceHistoricalLocationSessions(dayKey, login, locationSessions); err != nil {
			return HistoricalStudentDay{}, false, err
		}
		cachedSessions = locationSessions
	}
	if needAttendance {
		if attendanceErr != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not fetch Chronos attendance fallback for %s on %s: %v", login, dayKey, attendanceErr))
		} else if err := replaceHistoricalAttendanceBounds(dayKey, login, attendanceBounds); err != nil {
			return HistoricalStudentDay{}, false, err
		} else {
			cachedAttendanceBounds = attendanceBounds
		}
	} else if len(badgeEvents) > 0 && !attendanceFetched {
		if err := replaceHistoricalAttendanceBounds(dayKey, login, nil); err != nil {
			return HistoricalStudentDay{}, false, err
		}
	}
	NotifyLocationSessionsUpdate(login, dayKey)

	record, err = syntheticHistoricalStudentDay(login, dayKey, cachedSessions, cachedAttendanceBounds)
	if err != nil {
		return HistoricalStudentDay{}, false, err
	}
	Trace("BUILD", "historical student day load done for %s on %s: sessions=%d attendance_bounds=%t badge_events=%d", login, dayKey, len(cachedSessions), cachedAttendanceBounds != nil, len(badgeEvents))
	return record, true, nil
}

func HistoricalLocationSessionsForDay(login, dayKey string) ([]LocationSession, error) {
	ensureRuntimeDayState()
	return loadHistoricalLocationSessions(dayKey, login)
}

func CurrentUsers() ([]User, error) {
	ensureRuntimeDayState()
	return loadCurrentUsersForDay(currentRuntimeDayKey(), false)
}

func CurrentUserByLogin(login string) (User, bool, error) {
	ensureRuntimeDayState()
	return loadCurrentUserByLogin(currentRuntimeDayKey(), login)
}

func CurrentBadgeEvents(login string) ([]BadgeEvent, error) {
	ensureRuntimeDayState()
	return loadBadgeEventsForLogin(currentRuntimeDayKey(), login)
}

func UsersForDay(dayKey string, apprenticesOnly bool) ([]User, bool, error) {
	ensureRuntimeDayState()
	if strings.TrimSpace(dayKey) == "" || dayKey == currentRuntimeDayKey() {
		users, err := loadCurrentUsersForDay(currentRuntimeDayKey(), apprenticesOnly)
		return users, true, err
	}
	users, err := loadHistoricalUsersForDay(dayKey, apprenticesOnly)
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

func AttendancePostsForDay(dayKey string) (map[string][]AttendancePostRecord, error) {
	ensureRuntimeDayState()
	return loadAttendancePostsForDay(dayKey)
}

func DailyReportSummaries() ([]DailyReportSummary, error) {
	ensureRuntimeDayState()
	return loadDailyReportSummaries()
}

func AdminUserByLogin(login string) (AdminUserSummary, bool, error) {
	ensureRuntimeDayState()
	login = normalizeLogin(login)
	if login == "" {
		return AdminUserSummary{}, false, nil
	}

	var (
		user  AdminUserSummary
		found bool
		err   error
	)

	if user, found, err = loadLatestHistoricalAdminUserByLogin(login); err != nil {
		return AdminUserSummary{}, false, err
	}
	if currentProfile, ok, profileErr := loadCurrentAdminDayProfileByLogin(login); profileErr != nil {
		return AdminUserSummary{}, false, profileErr
	} else if ok {
		user = currentProfile
		found = true
	}
	if currentUser, ok, currentErr := loadCurrentAdminUserByLogin(login); currentErr != nil {
		return AdminUserSummary{}, false, currentErr
	} else if ok {
		user = currentUser
		found = true
	}

	return user, found, nil
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
	case "staff":
		return "staff"
	case "extern", "externe":
		return "extern"
	default:
		return ""
	}
}

func AdminUserStatusFromInput(status string) string {
	return normalizeManagedUserStatus(status)
}

func adminUserStatus(isApprentice bool, profile ProfileType) string {
	return statusFromSignals(isApprentice, profile)
}

func AdminUserStatus(isApprentice bool, profile ProfileType) string {
	return adminUserStatus(isApprentice, profile)
}

func statusLabel(isApprentice bool) string {
	return statusFromSignals(isApprentice, Student)
}

func normalizeManagedUserStatus(status string) string {
	return normalizeAdminUserStatus(status)
}

func coalesceManagedStatus(values ...string) string {
	for _, value := range values {
		if normalized := normalizeManagedUserStatus(value); normalized != "" {
			return normalized
		}
	}
	return "student"
}

func normalizeHistoricalSummaryStatus(user *User) {
	if user == nil {
		return
	}
	if normalizeManagedUserStatus(user.Status) != "" {
		return
	}
	if strings.TrimSpace(user.PostResult) == "" {
		user.PostResult = strings.TrimSpace(user.Status)
	}
	user.Status = statusFromSignals(user.IsApprentice, user.Profile)
}

func PopulateUserPostResult(user *User, posts []AttendancePostRecord) {
	if user == nil {
		return
	}

	user.PostResult = ""
	user.Error = nil

	if len(posts) > 0 {
		last := posts[len(posts)-1]
		if last.Success {
			user.PostResult = POSTED
			return
		}

		errorMessage := strings.TrimSpace(last.ErrorMessage)
		switch errorMessage {
		case "AUTOPOST is off":
			user.PostResult = POST_OFF
		case "user is blacklisted":
			user.PostResult = POST_SKIPPED_BLACKLIST
		case "badge posting is disabled":
			user.PostResult = POST_SKIPPED_DISABLED
		case "apprentice is not on a school day":
			user.PostResult = POST_SKIPPED_NOT_SCHOOL_DAY
		default:
			user.PostResult = POST_ERROR
		}

		if errorMessage != "" {
			user.Error = fmt.Errorf("%s", errorMessage)
		} else if strings.TrimSpace(last.ResponseStatus) != "" {
			user.Error = fmt.Errorf("%s", strings.TrimSpace(last.ResponseStatus))
		}
		return
	}

	if user.FirstAccess.IsZero() {
		if user.IsApprentice {
			user.PostResult = APPRENTICE_NO_BADGE
		} else {
			user.PostResult = NO_BADGE
		}
		return
	}

	if user.FirstAccess.Equal(user.LastAccess) {
		if user.IsApprentice {
			user.PostResult = APPRENTICE_BADGED_ONCE
		} else {
			user.PostResult = BADGED_ONCE
		}
		return
	}

	if !user.IsApprentice {
		user.PostResult = NOT_APPRENTICE
	}
}

func statusFromSignals(isApprentice bool, profile ProfileType) string {
	if isApprentice {
		return "apprentice"
	}
	switch profile {
	case Pisciner:
		return "pisciner"
	case Staff:
		return "staff"
	case Student:
		return "student"
	default:
		return "extern"
	}
}

func signalsFromStatus(status string) (bool, ProfileType) {
	switch normalizeManagedUserStatus(status) {
	case "apprentice":
		return true, Student
	case "pisciner":
		return false, Pisciner
	case "staff":
		return false, Staff
	case "extern":
		return false, 0
	default:
		return false, Student
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
