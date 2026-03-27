package watchdog

import (
	"fmt"
	"time"
)

type BadgeEvent struct {
	Timestamp time.Time `json:"timestamp"`
	DoorName  string    `json:"door_name"`
}

func parisLocation() *time.Location {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return time.Local
	}
	return loc
}

func dayKeyInParis(ts time.Time) string {
	return ts.In(parisLocation()).Format("2006-01-02")
}

func todayDayKeyInParis() string {
	return dayKeyInParis(time.Now())
}

func RecordDailyBadgeEvent(login string, ts time.Time, doorName string) {
	ensureRuntimeDayState()
	login = normalizeLogin(login)
	if login == "" {
		return
	}
	if dayKeyInParis(ts) != todayDayKeyInParis() {
		return
	}

	if err := saveBadgeEvent(todayDayKeyInParis(), login, ts, doorName); err != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not persist badge event for %s: %v", login, err))
	}
}

func SnapshotDailyBadgeEvents(login string) []BadgeEvent {
	ensureRuntimeDayState()
	login = normalizeLogin(login)
	if login == "" {
		return nil
	}

	events, err := CurrentBadgeEvents(login)
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not load badge events for %s: %v", login, err))
		return nil
	}
	return events
}

func DeleteDailyBadgeEvents(login string) {
	ensureRuntimeDayState()
	login = normalizeLogin(login)
	if login == "" {
		return
	}

	if err := deleteCurrentDayDataForLogin(todayDayKeyInParis(), login); err != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not delete persisted badge state for %s: %v", login, err))
	}
}
