package watchdog

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"watchdog/config"

	apiManager "github.com/TheKrainBow/go-api"
)

type cachedProfilePhoto struct {
	URL       string
	FetchedAt time.Time
}

var (
	profilePhotoCache      = make(map[string]cachedProfilePhoto)
	profilePhotoCacheMutex sync.Mutex
	profilePhotoFetches    = make(map[string]struct{})
)

const profilePhotoCacheTTL = 7 * 24 * time.Hour

func CachedUserPhotoURLOrSchedule(login string) string {
	login = normalizeLogin(login)
	if login == "" {
		return ""
	}

	now := time.Now()
	profilePhotoCacheMutex.Lock()
	if cached, ok := profilePhotoCache[login]; ok && now.Sub(cached.FetchedAt) < profilePhotoCacheTTL {
		profilePhotoCacheMutex.Unlock()
		return cached.URL
	}
	profilePhotoCacheMutex.Unlock()

	storedURL, storedFetchedAt, ok, err := loadStoredProfilePhoto(login)
	if err != nil {
		Log(fmt.Sprintf("[WATCHDOG] WARNING: could not load cached photo for %s: %v", login, err))
		scheduleProfilePhotoFetch(login)
		return ""
	}
	if ok {
		cacheProfilePhoto(login, storedURL, storedFetchedAt)
		if now.Sub(storedFetchedAt) < profilePhotoCacheTTL {
			return storedURL
		}
		scheduleProfilePhotoFetch(login)
		return storedURL
	}

	scheduleProfilePhotoFetch(login)
	return ""
}

func FetchUserPhotoURL(login string) (string, error) {
	login = normalizeLogin(login)
	if login == "" {
		return "", nil
	}

	storedURL, storedFetchedAt, ok, err := loadStoredProfilePhoto(login)
	if err != nil {
		return "", err
	}

	url, fetchedAt, err := fetchProfilePhotoFromAPI(login)
	if err != nil {
		if ok {
			cacheProfilePhoto(login, storedURL, storedFetchedAt)
			return storedURL, nil
		}
		return "", err
	}

	if err := saveStoredProfilePhoto(login, url, fetchedAt); err != nil {
		if ok {
			cacheProfilePhoto(login, storedURL, storedFetchedAt)
			return storedURL, nil
		}
		return "", err
	}
	cacheProfilePhoto(login, url, fetchedAt)
	return url, nil
}

func scheduleProfilePhotoFetch(login string) {
	profilePhotoCacheMutex.Lock()
	if _, exists := profilePhotoFetches[login]; exists {
		profilePhotoCacheMutex.Unlock()
		return
	}
	profilePhotoFetches[login] = struct{}{}
	profilePhotoCacheMutex.Unlock()

	go func() {
		defer func() {
			profilePhotoCacheMutex.Lock()
			delete(profilePhotoFetches, login)
			profilePhotoCacheMutex.Unlock()
		}()

		if _, err := FetchUserPhotoURL(login); err != nil {
			Log(fmt.Sprintf("[WATCHDOG] WARNING: could not fetch photo for %s in background: %v", login, err))
		}
	}()
}

func fetchProfilePhotoFromAPI(login string) (string, time.Time, error) {
	resp, err := apiManager.GetClient(config.FTv2).Get(fmt.Sprintf("/users/%s", login))
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("42 API returned %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, err
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", time.Time{}, err
	}

	url := extractPhotoURL(payload)
	return url, time.Now().UTC(), nil
}

func cacheProfilePhoto(login, url string, fetchedAt time.Time) {
	profilePhotoCacheMutex.Lock()
	profilePhotoCache[login] = cachedProfilePhoto{
		URL:       url,
		FetchedAt: fetchedAt,
	}
	profilePhotoCacheMutex.Unlock()
}

func loadStoredProfilePhoto(login string) (string, time.Time, bool, error) {
	if storageDB == nil {
		return "", time.Time{}, false, nil
	}

	var photoURL string
	var fetchedAtRaw sql.NullString
	err := storageQueryRow(`
		SELECT photo_url, fetched_at
		FROM watchdog_profile_photos
		WHERE login_42 = ?
	`, login).Scan(&photoURL, &fetchedAtRaw)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", time.Time{}, false, nil
		}
		return "", time.Time{}, false, err
	}
	if !fetchedAtRaw.Valid {
		return photoURL, time.Time{}, true, nil
	}
	fetchedAt, err := time.Parse(time.RFC3339Nano, fetchedAtRaw.String)
	if err != nil {
		return "", time.Time{}, false, err
	}
	return photoURL, fetchedAt, true, nil
}

func saveStoredProfilePhoto(login, url string, fetchedAt time.Time) error {
	if storageDB == nil {
		return nil
	}

	_, err := storageExec(`
		INSERT INTO watchdog_profile_photos (
			login_42, photo_url, fetched_at, updated_at
		) VALUES (?, ?, ?, ?)
		ON CONFLICT(login_42) DO UPDATE SET
			photo_url = excluded.photo_url,
			fetched_at = excluded.fetched_at,
			updated_at = excluded.updated_at
	`,
		login,
		url,
		fetchedAt.UTC().Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func extractPhotoURL(payload map[string]any) string {
	if payload == nil {
		return ""
	}

	if raw, ok := payload["image_url"].(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}

	image, ok := payload["image"].(map[string]any)
	if !ok {
		return ""
	}

	if versions, ok := image["versions"].(map[string]any); ok {
		for _, key := range []string{"small", "medium", "micro", "large"} {
			if raw, ok := versions[key].(string); ok && strings.TrimSpace(raw) != "" {
				return strings.TrimSpace(raw)
			}
		}
	}

	if raw, ok := image["link"].(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return ""
}
