package watchdog

import (
	"strings"
	"sync"
	"time"
)

type BadgeUpdateEvent struct {
	Login             string    `json:"login"`
	Timestamp         time.Time `json:"timestamp"`
	DoorName          string    `json:"door_name"`
	BadgeDelaySeconds int64     `json:"badge_delay_seconds"`
}

var badgeUpdateListeners []func(BadgeUpdateEvent)
var badgeUpdateListenersMutex sync.RWMutex

func RegisterBadgeUpdateListener(listener func(BadgeUpdateEvent)) {
	if listener == nil {
		return
	}

	badgeUpdateListenersMutex.Lock()
	defer badgeUpdateListenersMutex.Unlock()

	badgeUpdateListeners = append(badgeUpdateListeners, listener)
}

func NotifyBadgeUpdate(login string, ts time.Time, doorName string, badgeDelay time.Duration) {
	trimmedLogin := strings.ToLower(strings.TrimSpace(login))
	if trimmedLogin == "" || ts.IsZero() {
		return
	}

	event := BadgeUpdateEvent{
		Login:             trimmedLogin,
		Timestamp:         ts,
		DoorName:          strings.TrimSpace(doorName),
		BadgeDelaySeconds: int64(badgeDelay / time.Second),
	}

	badgeUpdateListenersMutex.RLock()
	listeners := append([]func(BadgeUpdateEvent){}, badgeUpdateListeners...)
	badgeUpdateListenersMutex.RUnlock()

	for _, listener := range listeners {
		listener(event)
	}
}
