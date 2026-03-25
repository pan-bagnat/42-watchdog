package watchdog

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"watchdog/config"
	"watchdog/mailer"

	apiManager "github.com/TheKrainBow/go-api"
)

var Recipients = []string{
	"heinz@42nice.fr",
	// "tac@42nice.fr",
}

type UserV2 struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
}

var acceptEvents bool = false
var acceptEventsMutex sync.Mutex

type TimePeriod struct {
	StartingTime time.Time
	EndingTime   time.Time
}

var watchtime map[time.Weekday][]TimePeriod
var currentTimePeriod *TimePeriod

func checkWatchtime() {
	Log("[WATCHDOG] ┌─ Watch periods")
	// Iterate over each day of the week
	for day := range 7 {
		periods := watchtime[time.Weekday(day)]
		Log(fmt.Sprintf("[WATCHDOG] ├── %s\n", time.Weekday(day)))

		var validPeriods []TimePeriod
		for i, period := range periods {
			log := strings.Builder{}
			log.WriteString(fmt.Sprintf("[WATCHDOG] ├─ %s -> %s", period.StartingTime.Format("15:04:05"), period.EndingTime.Format("15:04:05")))

			if period.StartingTime.After(period.EndingTime) {
				log.WriteString(" (Discarded, invalid time)")
				Log(log.String())
				continue
			}

			if i > 0 && periods[i-1].EndingTime.After(period.StartingTime) {
				log.WriteString(" (Discarded, overlapping periods)")
				Log(log.String())
				continue
			}

			if i > 0 && periods[i-1].EndingTime.After(period.StartingTime) {
				log.WriteString(" (Discarded, Period must be in chronological order)")
				Log(log.String())
				continue
			}
			validPeriods = append(validPeriods, period)
			Log(log.String())
		}

		if len(periods) == 0 {
			Log("[WATCHDOG] ├─ None")
		}
		watchtime[time.Weekday(day)] = validPeriods
	}
	Log("[WATCHDOG] └─ Done")
}

func InitWatchtime(watch map[time.Weekday][][]string) {
	watchtime = make(map[time.Weekday][]TimePeriod)
	for key, value := range watch {
		for _, ranges := range value {
			first, err := time.Parse("15:04:05", ranges[0])
			if err != nil {
				Log(fmt.Sprintf("[ERROR] Couldn't parse watchtime `%s`", ranges[0]))
				continue
			}
			last, err := time.Parse("15:04:05", ranges[1])
			if err != nil {
				Log(fmt.Sprintf("[ERROR] Couldn't parse watchtime `%s`", ranges[1]))
				continue
			}
			watchtime[key] = append(watchtime[key], TimePeriod{
				StartingTime: first,
				EndingTime:   last,
			})
		}
	}
	checkWatchtime()
}

func AfterTime(time1, time2 time.Time) bool {
	// Extract only the time parts (hour, minute, second)
	t1 := time.Date(0, 1, 1, time1.Hour(), time1.Minute(), time1.Second(), 0, time.Local)
	t2 := time.Date(0, 1, 1, time2.Hour(), time2.Minute(), time2.Second(), 0, time.Local)

	return t1.After(t2)
}

// BeforeTime checks if time1 is before time2 (ignoring dates)
func BeforeTime(time1, time2 time.Time) bool {
	// Extract only the time parts (hour, minute, second)
	t1 := time.Date(0, 1, 1, time1.Hour(), time1.Minute(), time1.Second(), 0, time.Local)
	t2 := time.Date(0, 1, 1, time2.Hour(), time2.Minute(), time2.Second(), 0, time.Local)

	return t1.Before(t2)
}

func getTimePeriodForTimeStamp(timeStamp time.Time) *TimePeriod {
	periods := watchtime[timeStamp.Weekday()]

	for i, period := range periods {
		if AfterTime(timeStamp, period.StartingTime) && BeforeTime(timeStamp, period.EndingTime) {
			return &periods[i]
		}
	}
	return nil
}

func FetchMissingFields(login string, userID string) (string, string) {
	resp, err := apiManager.GetClient(config.FTv2).Get(fmt.Sprintf("/users?filter[id]=%s&filter[login]=%s", userID, strings.ToLower(login)))
	if err != nil {
		Log(fmt.Sprintf("ERROR: %s\n", err.Error()))
		return login, userID
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		Log(fmt.Sprintf("ERROR: couldn't fetch user: %s\n", resp.Status))
		return login, userID
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		Log(fmt.Sprintf("ERROR: %s\n", err.Error()))
		return login, userID
	}

	var res []UserV2
	err = json.Unmarshal(respBytes, &res)
	if err != nil {
		Log(fmt.Sprintf("ERROR: %s", err.Error()))
		return login, userID
	}

	if len(res) == 0 {
		Log(fmt.Sprintf("ERROR: user (%s|%s) not found", login, userID))
		return login, userID
	}

	if len(res) > 1 {
		Log(fmt.Sprintf("ERROR: many user found with (%s|%s)", login, userID))
		return login, userID
	}

	return res[0].Login, strconv.FormatInt(int64(res[0].ID), 10)
}

func GetAllowEvents() bool {
	dest := false
	acceptEventsMutex.Lock()
	dest = acceptEvents
	acceptEventsMutex.Unlock()
	return dest
}

func AllowEvents(isAllowed bool) {
	if isAllowed {
		Log("[WATCHDOG] 🟢 accepting incoming events")
	} else {
		Log("[WATCHDOG] 🔴 refusing incoming events")
	}
	acceptEventsMutex.Lock()
	acceptEvents = isAllowed
	acceptEventsMutex.Unlock()
}

func GetBadgeByLogin(login string) int {
	if AllUsersMutex.TryLock() {
		AllUsersMutex.Lock()
		defer AllUsersMutex.Unlock()
	}
	for key, value := range AllUsers {
		if value.Login42 == login {
			return key
		}
	}
	return -1
}

func CreateNewUser(userID int, accessControlUsername string) (User, int, error) {
	user := User{
		ControlAccessID:   userID,
		ControlAccessName: accessControlUsername,
	}

	resp, err := apiManager.GetClient(config.AccessControl).Get(fmt.Sprintf("/users/%d", userID))
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] ERROR: %s", err.Error()))
		os.Exit(1)
	}

	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] ERROR: %s", err.Error()))
		os.Exit(1)
	}

	var res UserResponse
	err = json.Unmarshal(bodyBytes, &res)
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] ERROR: %s", err.Error()))
		os.Exit(1)
	}

	user.Login42 = res.Properties.Login
	user.ID42 = res.Properties.ID
	user.Profile = ProfileType(res.Profile)
	if user.Login42 == "" && user.ID42 == "" {
		return User{}, -1, fmt.Errorf("user (%s) has no Login42 AND no ID42", accessControlUsername)
	}

	if user.Login42 == "" || user.ID42 == "" {
		user.Login42, user.ID42 = FetchMissingFields(user.Login42, user.ID42)
	}

	if user.Login42 == "" || user.ID42 == "" {
		return User{}, -1, fmt.Errorf("failed to fetch Login42('%s') OR ID42('%s')", user.Login42, user.ID42)
	}

	badgeID := GetBadgeByLogin(user.Login42)
	if badgeID != -1 {
		return user, badgeID, nil
	}

	user.IsApprentice = false
	for _, projectID := range config.ConfigData.ApiV2.ApprenticeProjects {
		if isProjectOngoing(user.Login42, projectID) {
			user.IsApprentice = true
			break
		}
	}
	if user.IsApprentice && user.Profile != Student {
		Log(fmt.Sprintf("[WATCHDOG] ⚠️  Created a new user: %s is an apprentice with temporary badge", user.Login42))
	} else if user.IsApprentice {
		Log(fmt.Sprintf("[WATCHDOG] 📋 Created a new user: %s is an apprentice", user.Login42))
	} else if user.Profile == Pisciner {
		Log(fmt.Sprintf("[WATCHDOG] 📋 Created a new user: %s is a pisciner", user.Login42))
	} else if user.Profile == Student {
		Log(fmt.Sprintf("[WATCHDOG] 📋 Created a new user: %s is a basic student", user.Login42))
	} else if user.Profile == Staff {
		Log(fmt.Sprintf("[WATCHDOG] 📋 Created a new user: %s is a Staff", user.Login42))
	} else {
		Log(fmt.Sprintf("[WATCHDOG] 📋 Created a new user: %s is an extern", user.Login42))
	}
	return user, -1, nil
}

func UpdateUserAccess(userID int, accessControlUsername string, timeStamp time.Time, doorName string) {
	var err error
	var badge int
	AllUsersMutex.Lock()
	user, exist := AllUsers[userID]
	if !exist {
		user, badge, err = CreateNewUser(userID, accessControlUsername)
		if err != nil {
			Log(fmt.Sprintf("[WATCHDOG] ❌ Failed to create user: %s\n", err.Error()))
			AllUsersMutex.Unlock()
			return
		}
		if badge != -1 {
			Log(fmt.Sprintf("[WATCHDOG] ⚠️  User %s is already registered with another badge\n", user.Login42))
			if user.Profile == Student { // Badge that scanned is a student account. We need to replace the temporary badge with this one
				Log("[WATCHDOG] ⚠️  Stored badge is a temporary badge. Replacing with student badge\n")
				// Retrieve the user associated with the badge
				existingUser := AllUsers[badge]
				existingUser.Profile = Student
				existingUser.ControlAccessID = userID
				existingUser.ControlAccessName = accessControlUsername

				// Replace the temporary badge with the official badge
				AllUsers[userID] = existingUser // Assign the updated user to the new badge
				delete(AllUsers, badge)         // Remove the temporary badge entry
			} else {
				Log("[WATCHDOG] ⚠️  Used badge is a temporary badge. Logging User access on the real badge\n")
				user = AllUsers[badge]
				userID = badge
			}
		}
	}
	AllUsersMutex.Unlock()

	isInWatchtime := getTimePeriodForTimeStamp(timeStamp)
	if isInWatchtime != currentTimePeriod {
		if isInWatchtime != nil {
			Log(fmt.Sprintf("[WATCHDOG] 🕓 Watchtime changed: [%s - %s]", (*isInWatchtime).StartingTime.Format("15:04:05"), (*isInWatchtime).EndingTime.Format("15:04:05")))
		} else {
			Log("[WATCHDOG] 🕓 Watchtime changed: Watchdog went to sleep")
		}
		if currentTimePeriod != nil {
			PostApprenticesAttendances()
		}
		currentTimePeriod = isInWatchtime
	}

	if isInWatchtime == nil {
		Log(fmt.Sprintf("[WATCHDOG] 🚪 User %s used door %s at %s, but watchdog is sleeping", user.Login42, doorName, timeStamp.Format("15:04:05 MST")))
		return
	}

	if !acceptEvents {
		Log(fmt.Sprintf("[WATCHDOG] 🚪 User %s used door %s at %s, but events are not accepted", user.Login42, doorName, timeStamp.Format("15:04:05 MST")))
		return
	}

	Log(fmt.Sprintf("[WATCHDOG] 🚪 User %s used door %s at %s", user.Login42, doorName, timeStamp.Format("15:04:05 MST")))
	if user.FirstAccess.IsZero() || user.FirstAccess.After(timeStamp) {
		user.FirstAccess = timeStamp
		user.Duration = user.LastAccess.Sub(user.FirstAccess)
	}
	if user.LastAccess.IsZero() || user.LastAccess.Before(timeStamp) {
		user.LastAccess = timeStamp
		user.Duration = user.LastAccess.Sub(user.FirstAccess)
	}
	AllUsersMutex.Lock()
	AllUsers[userID] = user
	AllUsersMutex.Unlock()
}

func PrintUsersTimers() {
	parisLoc, _ := time.LoadLocation("Europe/Paris")
	AllUsersMutex.Lock()
	defer AllUsersMutex.Unlock()

	if len(AllUsers) == 0 {
		Log("[WATCHDOG] No users saved")
		return
	}

	Log("[WATCHDOG] ┌─ Users status:")

	var (
		naNoBadge []User // non-apprentices no badge
		naBadge   []User // non-apprentices with badge
		aNoBadge  []User // apprentices no badge
		aBadge    []User // apprentices with badge
	)

	for _, user := range AllUsers {
		switch {
		case !user.IsApprentice && user.FirstAccess.IsZero():
			naNoBadge = append(naNoBadge, user)
		case !user.IsApprentice && !user.FirstAccess.IsZero():
			naBadge = append(naBadge, user)
		case user.IsApprentice && user.FirstAccess.IsZero():
			aNoBadge = append(aNoBadge, user)
		case user.IsApprentice && !user.FirstAccess.IsZero():
			aBadge = append(aBadge, user)
		}
	}

	printUserGroup("├──────── Basic students: No badge usage", naNoBadge, false, parisLoc)
	printUserGroup("├──────── Basic students: Seen today", naBadge, true, parisLoc)
	printUserGroup("├──────── Apprentices:    No badge usage", aNoBadge, false, parisLoc)
	printUserGroup("├──────── Apprentices:    Seen today", aBadge, true, parisLoc)

	Log("[WATCHDOG] └─ Done")
}

func printUserGroup(title string, users []User, showTimes bool, loc *time.Location) {
	if len(users) == 0 {
		return
	}
	Log("[WATCHDOG] " + title)
	for _, user := range users {
		if showTimes {
			Log(fmt.Sprintf("[WATCHDOG] ├── %8s: %s -> %s ┆ Total : %s\n",
				user.Login42,
				user.FirstAccess.In(loc).Format("15h04m05s"),
				user.LastAccess.In(loc).Format("15h04m05s"),
				formatDuration(user.Duration),
			))
		} else {
			Log(fmt.Sprintf("[WATCHDOG] ├── %8s: %s ┆ Total : %s\n",
				user.Login42,
				"  No badge usage yet  ",
				formatDuration(user.Duration),
			))
		}
	}
}

func isProjectOngoing(login string, projectID string) bool {
	resp, err := apiManager.GetClient(config.FTv2).Get(fmt.Sprintf("/users/%s/projects/%s/teams?sort=-created_at", login, projectID))
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] ERROR: %s\n", err.Error()))
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] ERROR: %s\n", err.Error()))
		return false
	}

	var res []ProjectResponse
	err = json.Unmarshal(respBytes, &res)
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] ERROR: %s", err.Error()))
		os.Exit(1)
	}
	return len(res) >= 1 && res[0].Status == "in_progress"
}

type APIAttendance struct {
	Begin_at  string `json:"begin_at"`
	End_at    string `json:"end_at"`
	Source    string `json:"source"`
	Campus_id int    `json:"campus_id"`
	User_id   int    `json:"user_id"`
}

func getCampusID() int {
	if strings.TrimSpace(config.ConfigData.ApiV2.CampusID) == "" {
		return 41
	}
	campusID, err := strconv.Atoi(strings.TrimSpace(config.ConfigData.ApiV2.CampusID))
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: invalid campus ID %q, fallback to 41", config.ConfigData.ApiV2.CampusID))
		return 41
	}
	return campusID
}

func formatPostInfo(user User, loc *time.Location, msg string) string {
	first := "00:00:00"
	last := "00:00:00"
	if !user.FirstAccess.IsZero() {
		first = user.FirstAccess.In(loc).Format("15:04:05")
	}
	if !user.LastAccess.IsZero() {
		last = user.LastAccess.In(loc).Format("15:04:05")
	}
	emoji := "✅"
	if user.Status != POSTED {
		emoji = "❌"
	}
	return fmt.Sprintf(
		"[WATCHDOG] [POST] ├── %s %-8s: %s %s %s — %s\n",
		emoji,
		user.Login42,
		first,
		last,
		formatDuration(user.Duration),
		msg,
	)
}

func resetUserDuration(user User) {
	user.FirstAccess = time.Time{}
	user.LastAccess = time.Time{}
	user.Duration = 0
	AllUsers[user.ControlAccessID] = user
}

func SinglePostApprentice(user User) {
	parisLoc, _ := time.LoadLocation("Europe/Paris")
	defer func() {
		resetUserDuration(user)
		Log(formatPostInfo(user, parisLoc, user.Status))
	}()
	if user.FirstAccess.IsZero() {
		if user.IsApprentice {
			user.Status = APPRENTICE_NO_BADGE
		} else {
			user.Status = NO_BADGE
		}
		return
	}

	id42, _ := strconv.ParseInt(user.ID42, 10, 64)

	if user.FirstAccess.Equal(user.LastAccess) {
		if user.IsApprentice {
			user.Status = APPRENTICE_BADGED_ONCE
		} else {
			user.Status = BADGED_ONCE
		}
		return
	}

	if !user.IsApprentice {
		user.Status = NOT_APPRENTICE
		return
	}

	if !config.ConfigData.Attendance42.AutoPost {
		user.Status = POST_ERROR
		user.Error = fmt.Errorf("AUTOPOST is off")
		return
	}

	resp, err := apiManager.GetClient(config.FTAttendance).Post("/attendances", APIAttendance{
		Begin_at:  user.FirstAccess.UTC().Format(time.RFC3339),
		End_at:    user.LastAccess.UTC().Format(time.RFC3339),
		Source:    "access-control",
		Campus_id: getCampusID(),
		User_id:   int(id42),
	})

	if err != nil {
		user.Status = POST_ERROR
		user.Error = err
		return
	}

	if resp.StatusCode != http.StatusOK {
		user.Status = POST_ERROR
		user.Error = fmt.Errorf("%s", resp.Status)
		return
	}

	user.Status = POSTED
	user.Error = nil
}

func PostApprenticesAttendances() {
	parisLoc, _ := time.LoadLocation("Europe/Paris")
	sortedUser := map[string][]User{}
	AllUsersMutex.Lock()
	defer AllUsersMutex.Unlock()
	total := len(AllUsers)
	if total == 0 {
		Log("[WATCHDOG] [POST] Posting Attendances: no users registered")
		return
	}
	for _, user := range AllUsers {
		if user.FirstAccess.IsZero() {
			if user.IsApprentice {
				user.Status = APPRENTICE_NO_BADGE
			} else {
				user.Status = NO_BADGE
			}
			sortedUser[user.Status] = append(sortedUser[user.Status], user)
			resetUserDuration(user)
			continue
		}

		id42, _ := strconv.ParseInt(user.ID42, 10, 64)

		if user.FirstAccess.Equal(user.LastAccess) {
			if user.IsApprentice {
				user.Status = APPRENTICE_BADGED_ONCE
			} else {
				user.Status = BADGED_ONCE
			}
			sortedUser[user.Status] = append(sortedUser[user.Status], user)
			resetUserDuration(user)
			continue
		}

		if !user.IsApprentice {
			user.Status = NOT_APPRENTICE
			sortedUser[user.Status] = append(sortedUser[user.Status], user)
			resetUserDuration(user)
			continue
		}

		if !config.ConfigData.Attendance42.AutoPost {
			user.Status = POST_OFF
			sortedUser[user.Status] = append(sortedUser[user.Status], user)
			resetUserDuration(user)
			continue
		}

		resp, err := apiManager.GetClient(config.FTAttendance).Post("/attendances", APIAttendance{
			Begin_at:  user.FirstAccess.UTC().Format(time.RFC3339),
			End_at:    user.LastAccess.UTC().Format(time.RFC3339),
			Source:    "access-control",
			Campus_id: getCampusID(),
			User_id:   int(id42),
		})

		if err != nil {
			user.Status = POST_ERROR
			user.Error = err
			sortedUser[user.Status] = append(sortedUser[user.Status], user)
			resetUserDuration(user)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			user.Status = POST_ERROR
			user.Error = fmt.Errorf("%s", resp.Status)
			sortedUser[user.Status] = append(sortedUser[user.Status], user)
			resetUserDuration(user)
			continue
		}

		user.Status = POSTED
		user.Error = nil
		sortedUser[user.Status] = append(sortedUser[user.Status], user)
		resetUserDuration(user)
	}

	for status, users := range sortedUser {
		sort.Slice(users, func(i, j int) bool {
			return users[i].Login42 < users[j].Login42 // or .Login42, etc.
		})
		sortedUser[status] = users // update the map with the sorted slice
	}

	// LOG NON APPRENTICE USERS:

	Log("[WATCHDOG] [POST] ┌─ Posting Attendances:")
	if len(sortedUser[NO_BADGE]) > 0 {
		Log("[WATCHDOG] [POST] ├──────── Students: No badge used today")
		for _, user := range sortedUser[NO_BADGE] {
			Log(formatPostInfo(user, parisLoc, user.Status))
		}
	}

	if len(sortedUser[BADGED_ONCE]) > 0 {
		Log("[WATCHDOG] [POST] ├──────── Students: Used badge only once")
		for _, user := range sortedUser[BADGED_ONCE] {
			Log(formatPostInfo(user, parisLoc, user.Status))
		}
	}

	if len(sortedUser[NOT_APPRENTICE]) > 0 {
		Log("[WATCHDOG] [POST] ├──────── Students: Not an apprentice")
		for _, user := range sortedUser[NOT_APPRENTICE] {
			Log(formatPostInfo(user, parisLoc, user.Status))
		}
	}

	// LOG AND MAIL APPRENTICES:

	var htmlBody strings.Builder
	atLeastOneField := false
	today := time.Now()
	htmlBody.WriteString("<h2>Watchdog Daily Report – " + today.Format("02/01/2006") + "</h2>")
	htmlBody.WriteString(`
		<table style="border:2px solid #ccc; padding: 8px; border-collapse:collapse; background:#f9f9f9;">
	`)
	htmlBody.WriteString(`<tr><td style="white-space: pre; font-size: 13px; padding: 1px; padding-left: 20px; padding-right: 20px; line-height: 1;">  </td></tr>`)
	if len(sortedUser[POSTED]) > 0 {
		Log("[WATCHDOG] [POST] ├──────── Apprentices: Posts")
		for _, user := range sortedUser[POSTED] {
			Log(formatPostInfo(user, parisLoc, user.Status))
			addLogToMail(&htmlBody, user, parisLoc)
		}
		htmlBody.WriteString(`<tr><td style="white-space: pre; font-size: 13px; padding: 1px; padding-left: 20px; padding-right: 20px; line-height: 1;">  </td></tr>`)
		atLeastOneField = true
	}

	if len(sortedUser[POST_OFF]) > 0 {
		Log("[WATCHDOG] [POST] ├──────── Apprentices: Posts (off)")
		for _, user := range sortedUser[POST_OFF] {
			Log(formatPostInfo(user, parisLoc, user.Status))
			addLogToMail(&htmlBody, user, parisLoc)
		}
		htmlBody.WriteString(`<tr><td style="white-space: pre; font-size: 13px; padding: 1px; padding-left: 20px; padding-right: 20px; line-height: 1;">  </td></tr>`)
	}

	if len(sortedUser[POST_ERROR]) > 0 {
		Log("[WATCHDOG] [POST] ├──────── Apprentices: Posts errors")
		for _, user := range sortedUser[POST_ERROR] {
			Log(formatPostInfo(user, parisLoc, user.Error.Error()))
			addLogToMail(&htmlBody, user, parisLoc)
		}
		atLeastOneField = true
	}

	if len(sortedUser[APPRENTICE_BADGED_ONCE]) > 0 {
		Log("[WATCHDOG] [POST] ├──────── Apprentices: Used badge only once")
		for _, user := range sortedUser[APPRENTICE_BADGED_ONCE] {
			Log(formatPostInfo(user, parisLoc, "Used badge only once"))
			addLogToMail(&htmlBody, user, parisLoc)
		}
		atLeastOneField = true
	}

	if len(sortedUser[APPRENTICE_NO_BADGE]) > 0 {
		Log("[WATCHDOG] [POST] ├──────── Apprentices: No badge used today")
		for _, user := range sortedUser[APPRENTICE_NO_BADGE] {
			Log(formatPostInfo(user, parisLoc, "No badge used today"))
			addLogToMail(&htmlBody, user, parisLoc)
		}
		atLeastOneField = true
	}
	htmlBody.WriteString(`<tr><td style="white-space: pre; font-size: 13px; padding: 1px; padding-left: 20px; padding-right: 20px; line-height: 1;">  </td></tr>`)
	htmlBody.WriteString(`</table><p style="font-size:11px; color:#888;">Generated by Watchdog at ` + today.Format("15:04:05") + ` - Timezone is CEST &nbsp;</p>`)
	if atLeastOneField {
		mailer.Send(mailer.GetRecipients(), fmt.Sprintf("Watchdog – Daily Report - %s", time.Now().Format("02/01/2006")), htmlBody.String(), true)
	}

	for key, user := range AllUsers {
		user.Error = nil
		AllUsers[key] = user
	}
}

func addLogToMail(htmlBody *strings.Builder, user User, loc *time.Location) {
	color := "green"
	firstColor := "green"
	lastColor := "green"
	durationColor := "green"
	emoji := "✅"

	msg := user.Status
	if user.Error != nil {
		msg = user.Error.Error()
	}

	first := user.FirstAccess.In(loc)
	if first.Before(time.Date(first.Year(), first.Month(), first.Day(), 8, 0, 0, 0, loc)) {
		firstColor = "orange"
	}

	last := user.LastAccess.In(loc)
	if last.After(time.Date(last.Year(), last.Month(), last.Day(), 20, 0, 0, 0, loc)) {
		lastColor = "orange"
	}

	firstFormated := first.Format("15:04:05")
	if first.IsZero() {
		firstFormated = "00:00:00"
	}

	lastFormated := last.Format("15:04:05")
	if last.IsZero() {
		lastFormated = "00:00:00"
	}

	if user.Duration < 5*time.Hour {
		durationColor = "red"
	} else if user.Duration < 7*time.Hour {
		durationColor = "orange"
	}

	if user.Status != POSTED && user.Status != POST_OFF {
		color = "red"
		firstColor = "red"
		lastColor = "red"
		emoji = "❌"
		durationColor = "red"
	}

	htmlBody.WriteString(`<tr><td style="white-space: pre; font-family: Menlo, Consolas, 'Courier New', monospace; font-size: 13px; padding: 1px 20px; line-height: 1;">`)
	htmlBody.WriteString(`<span style="color: green;">` + emoji + `</span> `)
	htmlBody.WriteString(`<span style="color:` + color + `;">` + fmt.Sprintf("%-8s", user.Login42) + `</span>: `)
	htmlBody.WriteString(`<span style="color:` + firstColor + `;">` + firstFormated + `</span>-`)
	htmlBody.WriteString(`<span style="color:` + lastColor + `;">` + lastFormated + `</span> `)
	htmlBody.WriteString(`<span style="color:` + durationColor + `;">` + formatDuration(user.Duration) + `</span> — `)
	htmlBody.WriteString(`<span style="color:` + color + `;">` + msg + `</span>`)
	htmlBody.WriteString(`</td></tr>`)
}

func DeleteAllPisciners() {
	AllUsersMutex.Lock()
	defer AllUsersMutex.Unlock()

	for id, user := range AllUsers {
		if user.Profile == Pisciner {
			delete(AllUsers, id)
			Log(fmt.Sprintf("[WATCHDOG] 🗑️  Deleted pisciner %s from Watchdog", user.Login42))
		}
	}
}

func DeleteStudent(login string, withPost bool) {
	AllUsersMutex.Lock()
	defer AllUsersMutex.Unlock()

	for id, user := range AllUsers {
		if strings.EqualFold(user.Login42, login) {
			if withPost {
				SinglePostApprentice(user)
			}
			delete(AllUsers, id)
			if withPost {
				Log(fmt.Sprintf("[WATCHDOG] 🗑️  Deleted user %s from Watchdog with Post", user.Login42))
			} else {
				Log(fmt.Sprintf("[WATCHDOG] 🗑️  Deleted user %s from Watchdog", user.Login42))
			}
			return
		}
	}
	Log(fmt.Sprintf("[WATCHDOG] ⚠️  Could not delete user with login %s: user not found", login))
}

func UpdateStudent(login string, status bool) {
	AllUsersMutex.Lock()
	defer AllUsersMutex.Unlock()

	for id, user := range AllUsers {
		if strings.EqualFold(user.Login42, login) {
			user.IsApprentice = status
			AllUsers[id] = user
			Log(fmt.Sprintf("[WATCHDOG] 🔧 Manually updated %s to apprentice=%t", user.Login42, status))
			return
		}
	}
	Log(fmt.Sprintf("[WATCHDOG] ⚠️  Could not update user with login %s: user not found", login))
}

func RefetchStudent(login string) {
	AllUsersMutex.Lock()
	defer AllUsersMutex.Unlock()

	for id, user := range AllUsers {
		if strings.EqualFold(user.Login42, login) {
			oldStatus := user.IsApprentice
			user.IsApprentice = false
			for _, projectID := range config.ConfigData.ApiV2.ApprenticeProjects {
				if isProjectOngoing(user.Login42, projectID) {
					user.IsApprentice = true
					break
				}
			}
			AllUsers[id] = user
			Log(fmt.Sprintf("[WATCHDOG] 🔄 Refetched status for %s: %t → %t", user.Login42, oldStatus, user.IsApprentice))
			return
		}
	}

	Log(fmt.Sprintf("[WATCHDOG] ⚠️  Could not find user with login %s to refetch status", login))
}

func RefetchAllStudents() {
	Log("[WATCHDOG] 🔁 Refetching apprentice status for all users...")
	AllUsersMutex.Lock()
	defer AllUsersMutex.Unlock()

	for id, user := range AllUsers {
		oldStatus := user.IsApprentice
		user.IsApprentice = false
		for _, projectID := range config.ConfigData.ApiV2.ApprenticeProjects {
			if isProjectOngoing(user.Login42, projectID) {
				user.IsApprentice = true
				break
			}
		}
		AllUsers[id] = user
		if oldStatus != user.IsApprentice {
			Log(fmt.Sprintf("[WATCHDOG] 🔄 Updated %s: %t → %t", user.Login42, oldStatus, user.IsApprentice))
		}
	}

	Log("[WATCHDOG] ✅ All users' apprentice statuses have been refreshed")
}

func FindUserByLogin(login string) (User, bool) {
	AllUsersMutex.Lock()
	defer AllUsersMutex.Unlock()

	for _, user := range AllUsers {
		if strings.EqualFold(user.Login42, login) {
			return user, true
		}
	}
	return User{}, false
}

func SnapshotUsers() []User {
	AllUsersMutex.Lock()
	defer AllUsersMutex.Unlock()

	users := make([]User, 0, len(AllUsers))
	for _, user := range AllUsers {
		users = append(users, user)
	}
	sort.Slice(users, func(i, j int) bool {
		return strings.ToLower(users[i].Login42) < strings.ToLower(users[j].Login42)
	})
	return users
}
