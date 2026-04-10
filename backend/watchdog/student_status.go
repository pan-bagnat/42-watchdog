package watchdog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"watchdog/config"

	apiManager "github.com/TheKrainBow/go-api"
)

func fetchApprenticeStatus(login string) (bool, error) {
	for _, projectID := range config.ConfigData.ApiV2.ApprenticeProjects {
		ongoing, err := isProjectOngoing(login, projectID)
		if err != nil {
			return false, err
		}
		if ongoing {
			return true, nil
		}
	}
	return false, nil
}

func shouldRefreshStudentStatus(login string, ts time.Time) (bool, error) {
	login = normalizeLogin(login)
	if login == "" || ts.IsZero() {
		return false, nil
	}

	events, err := loadBadgeEventsForLogin(dayKeyInParis(ts), login)
	if err != nil {
		return false, err
	}
	if len(events) > 0 {
		return false, nil
	}

	state, ok, err := loadStudentStatusState(login)
	if err != nil {
		return false, err
	}
	if !ok || state.LastCheckedAt.IsZero() {
		return true, nil
	}

	return dayKeyInParis(state.LastCheckedAt) != dayKeyInParis(ts), nil
}

func refreshStudentStatusOnFirstBadge(user *User, ts time.Time) {
	if user == nil || user.Login42 == "" || user.Profile == Staff {
		return
	}

	shouldRefresh, err := shouldRefreshStudentStatus(user.Login42, ts)
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not evaluate status TTL for %s: %v", user.Login42, err))
		return
	}
	if !shouldRefresh {
		return
	}

	previous := user.IsApprentice
	next, err := fetchApprenticeStatus(user.Login42)
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not refresh status for %s: %v", user.Login42, err))
		if persistErr := saveStudentStatusState(user.Login42, user.IsApprentice, ts, nil); persistErr != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not persist status TTL for %s: %v", user.Login42, persistErr))
		}
		return
	}

	previousDetected := coalesceManagedStatus(user.Status42, statusFromSignals(previous, user.Profile))
	nextDetected := statusFromSignals(next, user.Profile)
	if previous != next {
		Log(fmt.Sprintf("[WATCHDOG] Detected a new status for %s: %s -> %s", user.Login42, statusLabel(previous), statusLabel(next)))
	}
	user.Status42 = nextDetected
	if user.StatusOverridden {
		if previousDetected != nextDetected {
			user.Status = nextDetected
			user.IsApprentice, user.Profile = signalsFromStatus(nextDetected)
		}
	} else {
		user.IsApprentice = next
		user.Status = user.Status42
	}

	if err := saveStudentStatusState(user.Login42, next, ts, &previous); err != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not persist status state for %s: %v", user.Login42, err))
	}
}

func forceStudentStatus(user *User, next bool, ts time.Time) {
	if user == nil || user.Login42 == "" {
		return
	}

	previous := user.IsApprentice
	user.IsApprentice = next

	if err := saveStudentStatusState(user.Login42, user.IsApprentice, ts, &previous); err != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not persist forced status for %s: %v", user.Login42, err))
	}
}

func isProjectOngoing(login string, projectID string) (bool, error) {
	resp, err := apiManager.GetClient(config.FTv2).Get(fmt.Sprintf("/users/%s/projects/%s/teams?sort=-created_at", login, projectID))
	if err != nil {
		return false, fmt.Errorf("42 API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, fmt.Errorf("42 API returned status %d for project %s: %s", resp.StatusCode, projectID, strings.TrimSpace(string(body)))
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("42 API read failed: %w", err)
	}

	var res []ProjectResponse
	if err := json.Unmarshal(respBytes, &res); err != nil {
		return false, fmt.Errorf("42 API decode failed: %w", err)
	}

	return len(res) >= 1 && res[0].Status == "in_progress", nil
}
