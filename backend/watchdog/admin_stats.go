package watchdog

import (
	"database/sql"
	"sort"
	"strings"
	"time"
)

const adminStatsLookbackDays = 30

type AdminStatsBucket struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

type AdminStatsSeries struct {
	Key    string             `json:"key"`
	Label  string             `json:"label"`
	Values []AdminStatsBucket `json:"values"`
}

type AdminStatsTypeCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

type AdminStatsDoorCount struct {
	DoorName string `json:"door_name"`
	Count    int    `json:"count"`
}

type AdminStatsResponse struct {
	SelectedWeekday             int                   `json:"selected_weekday"`
	PresenceWindowStart         string                `json:"presence_window_start"`
	PresenceWindowEnd           string                `json:"presence_window_end"`
	AverageSeenByWeekday        []AdminStatsBucket    `json:"average_seen_by_weekday"`
	AveragePresenceByHour       []AdminStatsBucket    `json:"average_presence_by_hour"`
	AveragePresenceByWeekday    []AdminStatsSeries    `json:"average_presence_by_weekday"`
	AveragePresenceByCategory   []AdminStatsSeries    `json:"average_presence_by_category"`
	UserTypeDistribution        []AdminStatsTypeCount `json:"user_type_distribution"`
	DoorUsage                   []AdminStatsDoorCount `json:"door_usage"`
	AverageDailyPresenceSeconds int64                 `json:"average_daily_presence_seconds"`
	ObservedDayCount            int                   `json:"observed_day_count"`
	FilteredUserCount           int                   `json:"filtered_user_count"`
	ProcessedUserCount          int                   `json:"processed_user_count"`
	TotalUserCount              int                   `json:"total_user_count"`
}

type adminStatsDayRecord struct {
	DayKey string
	Login  string
	First  time.Time
	Last   time.Time
}

func AdminStats(statuses []string, weekday int) (AdminStatsResponse, error) {
	ensureRuntimeDayState()

	if weekday < 0 || weekday > 6 {
		weekday = 0
	}

	users, err := loadAdminUsers("", statuses, "")
	if err != nil {
		return AdminStatsResponse{}, err
	}
	allUsers, err := loadAdminUsers("", nil, "")
	if err != nil {
		return AdminStatsResponse{}, err
	}

	filteredAllowedLogins := make(map[string]AdminUserSummary, len(users))
	filteredTypeCounts := map[string]int{
		"apprentice": 0,
		"student":    0,
		"pisciner":   0,
		"staff":      0,
		"extern":     0,
	}
	for _, user := range users {
		login := normalizeLogin(user.Login42)
		if login == "" {
			continue
		}
		status := coalesceManagedStatus(user.Status, statusFromSignals(user.IsApprentice, user.Profile))
		filteredAllowedLogins[login] = user
		filteredTypeCounts[status] += 1
	}

	allAllowedLogins := make(map[string]AdminUserSummary, len(allUsers))
	allStatusByLogin := make(map[string]string, len(allUsers))
	allTypeCounts := map[string]int{
		"apprentice": 0,
		"student":    0,
		"pisciner":   0,
		"staff":      0,
		"extern":     0,
	}
	for _, user := range allUsers {
		login := normalizeLogin(user.Login42)
		if login == "" {
			continue
		}
		status := coalesceManagedStatus(user.Status, statusFromSignals(user.IsApprentice, user.Profile))
		allAllowedLogins[login] = user
		allStatusByLogin[login] = status
		allTypeCounts[status] += 1
	}

	response := AdminStatsResponse{
		SelectedWeekday:     weekday,
		PresenceWindowStart: "00:00",
		PresenceWindowEnd:   "24:00",
		UserTypeDistribution: []AdminStatsTypeCount{
			{Status: "apprentice", Count: filteredTypeCounts["apprentice"]},
			{Status: "student", Count: filteredTypeCounts["student"]},
			{Status: "pisciner", Count: filteredTypeCounts["pisciner"]},
			{Status: "staff", Count: filteredTypeCounts["staff"]},
			{Status: "extern", Count: filteredTypeCounts["extern"]},
		},
		FilteredUserCount: len(filteredAllowedLogins),
		TotalUserCount:    len(allUsers),
	}
	if len(allAllowedLogins) == 0 {
		response.AverageSeenByWeekday = zeroWeekdayBuckets()
		response.AveragePresenceByHour = zeroHourBuckets()
		response.AveragePresenceByWeekday = zeroWeekdayHourSeries()
		response.AveragePresenceByCategory = zeroStatusHourSeries()
		response.DoorUsage = []AdminStatsDoorCount{}
		return response, nil
	}

	rangeStartDayKey, rangeEndDayKey := adminStatsDayRange()
	scheduleAdminStatsHistoricalMonthFetches(allAllowedLogins, rangeStartDayKey, rangeEndDayKey)
	fullyProcessedLogins, err := loadAdminStatsProcessedLogins(filteredAllowedLogins, rangeStartDayKey, rangeEndDayKey)
	if err != nil {
		return AdminStatsResponse{}, err
	}

	filteredRecords, observedDays, doorUsage, err := loadAdminStatsInputs(filteredAllowedLogins, rangeStartDayKey, rangeEndDayKey)
	if err != nil {
		return AdminStatsResponse{}, err
	}
	allRecords, allObservedDays, _, err := loadAdminStatsInputs(allAllowedLogins, rangeStartDayKey, rangeEndDayKey)
	if err != nil {
		return AdminStatsResponse{}, err
	}

	dayCounts := make(map[string]int, len(observedDays))
	for dayKey := range observedDays {
		dayCounts[dayKey] = 0
	}
	allDayCounts := make(map[string]int, len(allObservedDays))
	for dayKey := range allObservedDays {
		allDayCounts[dayKey] = 0
	}

	weekdaySums := make([]float64, 7)
	weekdayDayCounts := make([]int, 7)
	allWeekdayDayCounts := make([]int, 7)

	hourBuckets := buildStatsHourBuckets()
	hourSumsByWeekday := make([][]float64, 7)
	for index := range hourSumsByWeekday {
		hourSumsByWeekday[index] = make([]float64, len(hourBuckets))
	}
	statusOrder := []string{"student", "apprentice", "staff", "extern", "pisciner"}
	hourSumsByStatus := make(map[string][]float64, len(statusOrder))
	for _, status := range statusOrder {
		hourSumsByStatus[status] = make([]float64, len(hourBuckets))
	}

	var totalPresenceSeconds int64
	var totalPresenceSamples int64
	contributingProcessedLogins := make(map[string]struct{}, len(fullyProcessedLogins))

	for _, record := range filteredRecords {
		start, end, ok := clipStatsRange(record.DayKey, record.First, record.Last)
		if !ok {
			continue
		}

		dayCounts[record.DayKey] += 1

		durationSeconds := int64(end.Sub(start) / time.Second)
		if durationSeconds > 0 {
			totalPresenceSeconds += durationSeconds
			totalPresenceSamples += 1
		}
		if _, ok := fullyProcessedLogins[record.Login]; ok {
			contributingProcessedLogins[record.Login] = struct{}{}
		}

		parsedDay, ok := parseStatsDayKey(record.DayKey)
		if !ok {
			continue
		}
		weekdayIndex := statsWeekdayIndex(parsedDay)
		for index, bucket := range hourBuckets {
			if statsRangeOverlapsHourBucket(start, end, bucket) {
				hourSumsByWeekday[weekdayIndex][index] += 1
			}
		}
	}

	for _, record := range allRecords {
		start, end, ok := clipStatsRange(record.DayKey, record.First, record.Last)
		if !ok {
			continue
		}
		allDayCounts[record.DayKey] += 1
		parsedDay, ok := parseStatsDayKey(record.DayKey)
		if !ok {
			continue
		}
		weekdayIndex := statsWeekdayIndex(parsedDay)
		if weekdayIndex > 4 {
			continue
		}
		status := allStatusByLogin[record.Login]
		values, ok := hourSumsByStatus[status]
		if !ok {
			continue
		}
		for index, bucket := range hourBuckets {
			if statsRangeOverlapsHourBucket(start, end, bucket) {
				values[index] += 1
			}
		}
	}

	for dayKey, count := range dayCounts {
		if count <= 0 {
			continue
		}
		parsedDay, ok := parseStatsDayKey(dayKey)
		if !ok {
			continue
		}
		weekdayDayCounts[statsWeekdayIndex(parsedDay)] += 1
		weekdaySums[statsWeekdayIndex(parsedDay)] += float64(count)
	}
	for dayKey, count := range allDayCounts {
		if count <= 0 {
			continue
		}
		parsedDay, ok := parseStatsDayKey(dayKey)
		if !ok {
			continue
		}
		allWeekdayDayCounts[statsWeekdayIndex(parsedDay)] += 1
	}

	selectedWeekdayDayCount := weekdayDayCounts[weekday]

	response.AverageSeenByWeekday = make([]AdminStatsBucket, 0, 7)
	for index, label := range []string{"Lun", "Mar", "Mer", "Jeu", "Ven", "Sam", "Dim"} {
		value := 0.0
		if weekdayDayCounts[index] > 0 {
			value = weekdaySums[index] / float64(weekdayDayCounts[index])
		}
		response.AverageSeenByWeekday = append(response.AverageSeenByWeekday, AdminStatsBucket{
			Label: label,
			Value: value,
		})
	}

	response.AveragePresenceByHour = make([]AdminStatsBucket, 0, len(hourBuckets))
	for index, bucket := range hourBuckets {
		value := 0.0
		if selectedWeekdayDayCount > 0 {
			value = hourSumsByWeekday[weekday][index] / float64(selectedWeekdayDayCount)
		}
		response.AveragePresenceByHour = append(response.AveragePresenceByHour, AdminStatsBucket{
			Label: bucket.Label,
			Value: value,
		})
	}

	response.AveragePresenceByWeekday = make([]AdminStatsSeries, 0, 7)
	for weekdayIndex, label := range []string{"Lun", "Mar", "Mer", "Jeu", "Ven", "Sam", "Dim"} {
		series := AdminStatsSeries{
			Key:    strings.ToLower(label),
			Label:  label,
			Values: make([]AdminStatsBucket, 0, len(hourBuckets)),
		}
		for bucketIndex, bucket := range hourBuckets {
			value := 0.0
			if weekdayDayCounts[weekdayIndex] > 0 {
				value = hourSumsByWeekday[weekdayIndex][bucketIndex] / float64(weekdayDayCounts[weekdayIndex])
			}
			series.Values = append(series.Values, AdminStatsBucket{
				Label: bucket.Label,
				Value: value,
			})
		}
		response.AveragePresenceByWeekday = append(response.AveragePresenceByWeekday, series)
	}

	workdayCount := 0
	for weekdayIndex := 0; weekdayIndex <= 4; weekdayIndex += 1 {
		workdayCount += allWeekdayDayCounts[weekdayIndex]
	}

	response.AveragePresenceByCategory = make([]AdminStatsSeries, 0, len(statusOrder))
	for _, status := range statusOrder {
		series := AdminStatsSeries{
			Key:    status,
			Label:  status,
			Values: make([]AdminStatsBucket, 0, len(hourBuckets)),
		}
		statusPopulation := allTypeCounts[status]
		for bucketIndex, bucket := range hourBuckets {
			value := 0.0
			if workdayCount > 0 && statusPopulation > 0 {
				value = hourSumsByStatus[status][bucketIndex] / float64(workdayCount*statusPopulation)
			}
			series.Values = append(series.Values, AdminStatsBucket{
				Label: bucket.Label,
				Value: value,
			})
		}
		response.AveragePresenceByCategory = append(response.AveragePresenceByCategory, series)
	}

	response.DoorUsage = make([]AdminStatsDoorCount, 0, len(doorUsage))
	for doorName, count := range doorUsage {
		response.DoorUsage = append(response.DoorUsage, AdminStatsDoorCount{
			DoorName: doorName,
			Count:    count,
		})
	}
	sort.Slice(response.DoorUsage, func(i, j int) bool {
		if response.DoorUsage[i].Count == response.DoorUsage[j].Count {
			return response.DoorUsage[i].DoorName < response.DoorUsage[j].DoorName
		}
		return response.DoorUsage[i].Count > response.DoorUsage[j].Count
	})

	if totalPresenceSamples > 0 {
		response.AverageDailyPresenceSeconds = totalPresenceSeconds / totalPresenceSamples
	}
	response.ObservedDayCount = len(observedDays)
	response.ProcessedUserCount = len(contributingProcessedLogins)

	return response, nil
}

func loadAdminStatsInputs(allowed map[string]AdminUserSummary, startDayKey, endDayKey string) (map[string]*adminStatsDayRecord, map[string]struct{}, map[string]int, error) {
	records := make(map[string]*adminStatsDayRecord)
	observedDays := make(map[string]struct{})
	doorUsage := make(map[string]int)

	if len(allowed) == 0 {
		return records, observedDays, doorUsage, nil
	}
	if err := loadAdminStatsSummaryRanges(allowed, records, observedDays, startDayKey, endDayKey); err != nil {
		return nil, nil, nil, err
	}
	if err := loadAdminStatsCurrentRanges(allowed, records, observedDays, startDayKey, endDayKey); err != nil {
		return nil, nil, nil, err
	}
	if err := loadAdminStatsAttendanceBounds(allowed, records, observedDays, startDayKey, endDayKey); err != nil {
		return nil, nil, nil, err
	}
	if err := loadAdminStatsLocationRanges(allowed, records, observedDays, startDayKey, endDayKey); err != nil {
		return nil, nil, nil, err
	}
	if err := loadAdminStatsBadgeEvents(allowed, records, observedDays, doorUsage, startDayKey, endDayKey); err != nil {
		return nil, nil, nil, err
	}

	return records, observedDays, doorUsage, nil
}

type statsHourBucket struct {
	Label        string
	StartMinutes float64
	EndMinutes   float64
}

func zeroWeekdayBuckets() []AdminStatsBucket {
	return []AdminStatsBucket{
		{Label: "Lun", Value: 0},
		{Label: "Mar", Value: 0},
		{Label: "Mer", Value: 0},
		{Label: "Jeu", Value: 0},
		{Label: "Ven", Value: 0},
		{Label: "Sam", Value: 0},
		{Label: "Dim", Value: 0},
	}
}

func zeroHourBuckets() []AdminStatsBucket {
	buckets := buildStatsHourBuckets()
	zeroes := make([]AdminStatsBucket, 0, len(buckets))
	for _, bucket := range buckets {
		zeroes = append(zeroes, AdminStatsBucket{Label: bucket.Label, Value: 0})
	}
	return zeroes
}

func zeroWeekdayHourSeries() []AdminStatsSeries {
	weekdayLabels := []string{"Lun", "Mar", "Mer", "Jeu", "Ven", "Sam", "Dim"}
	series := make([]AdminStatsSeries, 0, len(weekdayLabels))
	for _, label := range weekdayLabels {
		series = append(series, AdminStatsSeries{
			Key:    strings.ToLower(label),
			Label:  label,
			Values: zeroHourBuckets(),
		})
	}
	return series
}

func zeroStatusHourSeries() []AdminStatsSeries {
	statuses := []string{"student", "apprentice", "staff", "extern", "pisciner"}
	series := make([]AdminStatsSeries, 0, len(statuses))
	for _, status := range statuses {
		series = append(series, AdminStatsSeries{
			Key:    status,
			Label:  status,
			Values: zeroHourBuckets(),
		})
	}
	return series
}

func buildStatsHourBuckets() []statsHourBucket {
	day, _ := parseStatsDayKey("2000-01-03")
	buckets := make([]statsHourBucket, 0, 24)
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	for index := 0; index < 24; index += 1 {
		bucketStart := start.Add(time.Duration(index) * time.Hour)
		buckets = append(buckets, statsHourBucket{
			Label:        bucketStart.Format("15:04"),
			StartMinutes: statsMinutesOfDay(bucketStart),
			EndMinutes:   statsMinutesOfDay(bucketStart.Add(time.Hour)),
		})
	}
	return buckets
}

func loadAdminStatsSummaryRanges(allowed map[string]AdminUserSummary, records map[string]*adminStatsDayRecord, observedDays map[string]struct{}, startDayKey, endDayKey string) error {
	if storageDB == nil {
		return nil
	}

	rows, err := storageQuery(`
		SELECT day_key, login_42, first_access, last_access
		FROM watchdog_daily_student_summaries
		WHERE day_key BETWEEN ? AND ?
	`, startDayKey, endDayKey)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var dayKey string
		var login string
		var firstRaw sql.NullString
		var lastRaw sql.NullString
		if err := rows.Scan(&dayKey, &login, &firstRaw, &lastRaw); err != nil {
			return err
		}
		login = normalizeLogin(login)
		if _, ok := allowed[login]; !ok {
			continue
		}
		observedDays[dayKey] = struct{}{}
		addStatsRange(records, dayKey, login, parseStatsTime(firstRaw), parseStatsTime(lastRaw))
	}

	return rows.Err()
}

func loadAdminStatsCurrentRanges(allowed map[string]AdminUserSummary, records map[string]*adminStatsDayRecord, observedDays map[string]struct{}, startDayKey, endDayKey string) error {
	if storageDB == nil {
		return nil
	}

	rows, err := storageQuery(`
		SELECT day_key, login_42, first_access, last_access
		FROM watchdog_current_users
		WHERE day_key BETWEEN ? AND ?
	`, startDayKey, endDayKey)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var dayKey string
		var login string
		var firstRaw sql.NullString
		var lastRaw sql.NullString
		if err := rows.Scan(&dayKey, &login, &firstRaw, &lastRaw); err != nil {
			return err
		}
		login = normalizeLogin(login)
		if _, ok := allowed[login]; !ok {
			continue
		}
		observedDays[dayKey] = struct{}{}
		addStatsRange(records, dayKey, login, parseStatsTime(firstRaw), parseStatsTime(lastRaw))
	}

	return rows.Err()
}

func loadAdminStatsAttendanceBounds(allowed map[string]AdminUserSummary, records map[string]*adminStatsDayRecord, observedDays map[string]struct{}, startDayKey, endDayKey string) error {
	if storageDB == nil {
		return nil
	}

	rows, err := storageQuery(`
		SELECT day_key, login_42, begin_at, end_at
		FROM watchdog_daily_attendance_bounds
		WHERE day_key BETWEEN ? AND ?
	`, startDayKey, endDayKey)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var dayKey string
		var login string
		var beginRaw string
		var endRaw string
		if err := rows.Scan(&dayKey, &login, &beginRaw, &endRaw); err != nil {
			return err
		}
		login = normalizeLogin(login)
		if _, ok := allowed[login]; !ok {
			continue
		}
		beginAt, beginOK := parseStatsRawTime(beginRaw)
		endAt, endOK := parseStatsRawTime(endRaw)
		if !beginOK || !endOK {
			continue
		}
		observedDays[dayKey] = struct{}{}
		addStatsRange(records, dayKey, login, beginAt, endAt)
	}

	return rows.Err()
}

func loadAdminStatsLocationRanges(allowed map[string]AdminUserSummary, records map[string]*adminStatsDayRecord, observedDays map[string]struct{}, startDayKey, endDayKey string) error {
	if storageDB == nil {
		return nil
	}

	rows, err := storageQuery(`
		SELECT day_key, login_42, begin_at, end_at
		FROM watchdog_daily_location_sessions
		WHERE day_key BETWEEN ? AND ?
	`, startDayKey, endDayKey)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var dayKey string
		var login string
		var beginRaw string
		var endRaw string
		if err := rows.Scan(&dayKey, &login, &beginRaw, &endRaw); err != nil {
			return err
		}
		login = normalizeLogin(login)
		if _, ok := allowed[login]; !ok {
			continue
		}
		beginAt, beginOK := parseStatsRawTime(beginRaw)
		endAt, endOK := parseStatsRawTime(endRaw)
		if !beginOK || !endOK {
			continue
		}
		observedDays[dayKey] = struct{}{}
		addStatsRange(records, dayKey, login, beginAt, endAt)
	}

	return rows.Err()
}

func loadAdminStatsBadgeEvents(allowed map[string]AdminUserSummary, records map[string]*adminStatsDayRecord, observedDays map[string]struct{}, doorUsage map[string]int, startDayKey, endDayKey string) error {
	if storageDB == nil {
		return nil
	}

	rows, err := storageQuery(`
		SELECT day_key, login_42, occurred_at, door_name
		FROM watchdog_badge_events
		WHERE day_key BETWEEN ? AND ?
	`, startDayKey, endDayKey)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var dayKey string
		var login string
		var occurredAtRaw string
		var doorName string
		if err := rows.Scan(&dayKey, &login, &occurredAtRaw, &doorName); err != nil {
			return err
		}
		login = normalizeLogin(login)
		if _, ok := allowed[login]; !ok {
			continue
		}
		occurredAt, ok := parseStatsRawTime(occurredAtRaw)
		if !ok {
			continue
		}
		observedDays[dayKey] = struct{}{}
		addStatsRange(records, dayKey, login, occurredAt, occurredAt)
		trimmedDoor := strings.TrimSpace(doorName)
		if trimmedDoor == "" {
			trimmedDoor = "Inconnue"
		}
		doorUsage[trimmedDoor] += 1
	}

	return rows.Err()
}

func addStatsRange(records map[string]*adminStatsDayRecord, dayKey, login string, start, end time.Time) {
	if strings.TrimSpace(dayKey) == "" || strings.TrimSpace(login) == "" {
		return
	}
	if start.IsZero() && end.IsZero() {
		return
	}
	if start.IsZero() {
		start = end
	}
	if end.IsZero() {
		end = start
	}
	if end.Before(start) {
		start, end = end, start
	}

	key := dayKey + "\x00" + login
	record, ok := records[key]
	if !ok {
		records[key] = &adminStatsDayRecord{
			DayKey: dayKey,
			Login:  login,
			First:  start,
			Last:   end,
		}
		return
	}

	if record.First.IsZero() || start.Before(record.First) {
		record.First = start
	}
	if record.Last.IsZero() || end.After(record.Last) {
		record.Last = end
	}
}

func adminStatsDayRange() (string, string) {
	endDayKey := todayDayKeyInParis()
	endDay, ok := parseStatsDayKey(endDayKey)
	if !ok {
		now := time.Now().In(parisLocation())
		endDay = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		endDayKey = endDay.Format("2006-01-02")
	}
	startDay := endDay.AddDate(0, 0, -(adminStatsLookbackDays - 1))
	return startDay.Format("2006-01-02"), endDayKey
}

func scheduleAdminStatsHistoricalMonthFetches(allowed map[string]AdminUserSummary, startDayKey, endDayKey string) {
	if storageDB == nil || len(allowed) == 0 {
		return
	}

	historicalDayKeys, err := adminStatsHistoricalDayKeys(startDayKey, endDayKey)
	if err != nil || len(historicalDayKeys) == 0 {
		return
	}

	fetchedDaysByLogin, err := loadAdminStatsAttendanceFetchMarkers(allowed, startDayKey, endDayKey)
	if err != nil {
		Log("[WATCHDOG] WARNING: could not inspect historical monthly attendance markers for admin stats preload: " + err.Error())
		return
	}

	missingByLoginMonth := make(map[string]map[string][]string, len(allowed))
	for login := range allowed {
		for _, dayKey := range historicalDayKeys {
			if fetchedDays, ok := fetchedDaysByLogin[login]; ok {
				if _, fetched := fetchedDays[dayKey]; fetched {
					continue
				}
			}
			monthKey := dayKey[:7]
			if _, ok := missingByLoginMonth[login]; !ok {
				missingByLoginMonth[login] = make(map[string][]string, 2)
			}
			missingByLoginMonth[login][monthKey] = append(missingByLoginMonth[login][monthKey], dayKey)
		}
	}

	for login, months := range missingByLoginMonth {
		for monthKey, dayKeys := range months {
			if len(dayKeys) == 0 {
				continue
			}
			enqueueHistoricalMonthFetch(login, monthKey, dayKeys)
		}
	}
}

func adminStatsHistoricalDayKeys(startDayKey, endDayKey string) ([]string, error) {
	startDay, ok := parseStatsDayKey(startDayKey)
	if !ok {
		return nil, sql.ErrNoRows
	}
	endDay, ok := parseStatsDayKey(endDayKey)
	if !ok {
		return nil, sql.ErrNoRows
	}
	todayKey := todayDayKeyInParis()
	dayKeys := make([]string, 0, adminStatsLookbackDays)
	for day := startDay; !day.After(endDay); day = day.AddDate(0, 0, 1) {
		dayKey := day.Format("2006-01-02")
		if dayKey >= todayKey {
			break
		}
		dayKeys = append(dayKeys, dayKey)
	}
	return dayKeys, nil
}

func loadAdminStatsAttendanceFetchMarkers(allowed map[string]AdminUserSummary, startDayKey, endDayKey string) (map[string]map[string]struct{}, error) {
	if storageDB == nil || len(allowed) == 0 {
		return map[string]map[string]struct{}{}, nil
	}

	logins := make([]string, 0, len(allowed))
	for login := range allowed {
		logins = append(logins, normalizeLogin(login))
	}
	sort.Strings(logins)

	placeholders := make([]string, 0, len(logins))
	args := make([]any, 0, len(logins)+2)
	args = append(args, startDayKey, endDayKey)
	for _, login := range logins {
		placeholders = append(placeholders, "?")
		args = append(args, login)
	}

	rows, err := storageQuery(`
		SELECT login_42, day_key
		FROM watchdog_historical_attendance_fetches
		WHERE day_key BETWEEN ? AND ?
		  AND login_42 IN (`+strings.Join(placeholders, ",")+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fetchedByLogin := make(map[string]map[string]struct{}, len(logins))
	for rows.Next() {
		var login string
		var dayKey string
		if err := rows.Scan(&login, &dayKey); err != nil {
			return nil, err
		}
		login = normalizeLogin(login)
		if _, ok := fetchedByLogin[login]; !ok {
			fetchedByLogin[login] = make(map[string]struct{})
		}
		fetchedByLogin[login][dayKey] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return fetchedByLogin, nil
}

func loadAdminStatsLocationFetchMarkers(allowed map[string]AdminUserSummary, startDayKey, endDayKey string) (map[string]map[string]struct{}, error) {
	if storageDB == nil || len(allowed) == 0 {
		return map[string]map[string]struct{}{}, nil
	}

	logins := make([]string, 0, len(allowed))
	for login := range allowed {
		logins = append(logins, normalizeLogin(login))
	}
	sort.Strings(logins)

	placeholders := make([]string, 0, len(logins))
	args := make([]any, 0, len(logins)+2)
	args = append(args, startDayKey, endDayKey)
	for _, login := range logins {
		placeholders = append(placeholders, "?")
		args = append(args, login)
	}

	rows, err := storageQuery(`
		SELECT login_42, day_key
		FROM watchdog_historical_location_fetches
		WHERE day_key BETWEEN ? AND ?
		  AND login_42 IN (`+strings.Join(placeholders, ",")+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fetchedByLogin := make(map[string]map[string]struct{}, len(logins))
	for rows.Next() {
		var login string
		var dayKey string
		if err := rows.Scan(&login, &dayKey); err != nil {
			return nil, err
		}
		login = normalizeLogin(login)
		if _, ok := fetchedByLogin[login]; !ok {
			fetchedByLogin[login] = make(map[string]struct{})
		}
		fetchedByLogin[login][dayKey] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return fetchedByLogin, nil
}

func loadAdminStatsProcessedLogins(allowed map[string]AdminUserSummary, startDayKey, endDayKey string) (map[string]struct{}, error) {
	if len(allowed) == 0 {
		return map[string]struct{}{}, nil
	}

	historicalDayKeys, err := adminStatsHistoricalDayKeys(startDayKey, endDayKey)
	if err != nil {
		return nil, err
	}
	if len(historicalDayKeys) == 0 {
		processed := make(map[string]struct{}, len(allowed))
		for login := range allowed {
			processed[normalizeLogin(login)] = struct{}{}
		}
		return processed, nil
	}

	attendanceFetchedByLogin, err := loadAdminStatsAttendanceFetchMarkers(allowed, startDayKey, endDayKey)
	if err != nil {
		return nil, err
	}
	locationFetchedByLogin, err := loadAdminStatsLocationFetchMarkers(allowed, startDayKey, endDayKey)
	if err != nil {
		return nil, err
	}

	processed := make(map[string]struct{}, len(allowed))
	for login := range allowed {
		normalizedLogin := normalizeLogin(login)
		attendanceFetchedDays := attendanceFetchedByLogin[normalizedLogin]
		locationFetchedDays := locationFetchedByLogin[normalizedLogin]
		complete := true
		for _, dayKey := range historicalDayKeys {
			if _, ok := attendanceFetchedDays[dayKey]; !ok {
				complete = false
				break
			}
			if _, ok := locationFetchedDays[dayKey]; !ok {
				complete = false
				break
			}
		}
		if complete {
			processed[normalizedLogin] = struct{}{}
		}
	}

	return processed, nil
}

func parseStatsTime(raw sql.NullString) time.Time {
	if !raw.Valid {
		return time.Time{}
	}
	parsed, ok := parseStatsRawTime(raw.String)
	if !ok {
		return time.Time{}
	}
	return parsed
}

func parseStatsRawTime(raw string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func parseStatsDayKey(dayKey string) (time.Time, bool) {
	loc := parisLocation()
	parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dayKey), loc)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func statsWeekdayIndex(day time.Time) int {
	return (int(day.Weekday()) + 6) % 7
}

func statsWindowBounds(dayKey string) (time.Time, time.Time, bool) {
	day, ok := parseStatsDayKey(dayKey)
	if !ok {
		return time.Time{}, time.Time{}, false
	}

	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location()),
		time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, day.Location()),
		true
}

func clipStatsRange(dayKey string, start, end time.Time) (time.Time, time.Time, bool) {
	if start.IsZero() || end.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	if end.Before(start) {
		start, end = end, start
	}

	windowStart, windowEnd, ok := statsWindowBounds(dayKey)
	if !ok {
		return time.Time{}, time.Time{}, false
	}

	clippedStart := start.In(windowStart.Location())
	clippedEnd := end.In(windowStart.Location())
	if clippedStart.Before(windowStart) {
		clippedStart = windowStart
	}
	if clippedEnd.After(windowEnd) {
		clippedEnd = windowEnd
	}
	if !clippedEnd.After(clippedStart) {
		return time.Time{}, time.Time{}, false
	}

	return clippedStart, clippedEnd, true
}

func statsMinutesOfDay(value time.Time) float64 {
	return float64(value.Hour()*60+value.Minute()) + float64(value.Second())/60 + float64(value.Nanosecond())/float64(time.Minute)
}

func statsRangeOverlapsHourBucket(start, end time.Time, bucket statsHourBucket) bool {
	startMinutes := statsMinutesOfDay(start)
	endMinutes := statsMinutesOfDay(end)
	return startMinutes < bucket.EndMinutes && endMinutes > bucket.StartMinutes
}
