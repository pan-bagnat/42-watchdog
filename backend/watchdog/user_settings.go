package watchdog

import (
	"fmt"
	"time"
)

type UserSettingsPatch struct {
	IsBlacklisted    *bool
	IsContributor    *bool
	BlacklistReason  *string
	Status           *string
	StatusOverridden *bool
}

func UserSettingsByLogin(login string) (UserSettings, bool, error) {
	ensureRuntimeDayState()
	return loadUserSettings(login)
}

func EnsureKnownUser(login string, ftIsStaff bool) error {
	ensureRuntimeDayState()
	stateMutationMutex.Lock()
	defer stateMutationMutex.Unlock()

	login = normalizeLogin(login)
	if login == "" {
		return nil
	}

	dayKey := currentRuntimeDayKey()
	dayProfiles, err := loadDayProfiles(dayKey)
	if err != nil {
		return err
	}
	if _, ok := dayProfiles[login]; ok {
		return nil
	}

	user := User{
		Login42:      login,
		IsApprentice: false,
		Profile:      Student,
	}
	if ftIsStaff {
		user.Profile = Staff
	}

	if state, ok, err := loadStudentStatusState(login); err != nil {
		return err
	} else if ok {
		user.IsApprentice = state.IsApprentice
	} else if !ftIsStaff {
		nextStatus, fetchErr := fetchApprenticeStatus(login)
		if fetchErr != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not fetch initial status for %s: %v", login, fetchErr))
		} else {
			user.IsApprentice = nextStatus
			if err := saveStudentStatusState(login, nextStatus, time.Now(), nil); err != nil {
				Log(fmt.Sprintf("[WATCHDOG] WARNING: could not persist initial status for %s: %v", login, err))
			}
		}
	}
	user.Status = statusFromSignals(user.IsApprentice, user.Profile)
	user.Status42 = user.Status

	return saveDayProfile(dayKey, user)
}

func UpdateUserSettings(login string, patch UserSettingsPatch) (UserSettings, error) {
	ensureRuntimeDayState()
	stateMutationMutex.Lock()
	defer stateMutationMutex.Unlock()

	settings, ok, err := loadUserSettings(login)
	if err != nil {
		return UserSettings{}, err
	}
	if !ok {
		settings = UserSettings{
			Login42:  normalizeLogin(login),
			Status:   "student",
			Status42: "student",
		}
	}

	if patch.IsBlacklisted != nil {
		settings.IsBlacklisted = *patch.IsBlacklisted
	}
	if patch.IsContributor != nil {
		settings.IsContributor = *patch.IsContributor
	}
	if patch.BlacklistReason != nil {
		settings.BlacklistReason = *patch.BlacklistReason
	}
	if patch.StatusOverridden != nil {
		settings.StatusOverridden = *patch.StatusOverridden
	}
	if patch.Status != nil {
		settings.Status = coalesceManagedStatus(*patch.Status, settings.Status42)
	}
	if !settings.StatusOverridden {
		settings.Status = coalesceManagedStatus(settings.Status42, settings.Status)
	}

	if err := saveUserSettings(settings); err != nil {
		return UserSettings{}, err
	}

	return settings, nil
}

func userPostBlockedReason(user User) string {
	switch {
	case user.IsBlacklisted:
		return "blacklisted"
	case user.BadgePostingOff:
		return "disabled"
	default:
		return ""
	}
}

func logUserSettingsChange(login string, field string, from string, to string) {
	Log(fmt.Sprintf("[WATCHDOG] Updated %s for %s: %s -> %s", field, normalizeLogin(login), from, to))
}

func nowUTC() time.Time {
	return time.Now().UTC()
}
