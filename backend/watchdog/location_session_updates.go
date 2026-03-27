package watchdog

import (
	"strings"
	"sync"
)

type LocationSessionsUpdateEvent struct {
	Login  string `json:"login"`
	DayKey string `json:"day"`
}

var locationSessionsUpdateListeners []func(LocationSessionsUpdateEvent)
var locationSessionsUpdateListenersMutex sync.RWMutex

func RegisterLocationSessionsUpdateListener(listener func(LocationSessionsUpdateEvent)) {
	if listener == nil {
		return
	}

	locationSessionsUpdateListenersMutex.Lock()
	defer locationSessionsUpdateListenersMutex.Unlock()

	locationSessionsUpdateListeners = append(locationSessionsUpdateListeners, listener)
}

func NotifyLocationSessionsUpdate(login, dayKey string) {
	trimmedLogin := strings.ToLower(strings.TrimSpace(login))
	trimmedDayKey := strings.TrimSpace(dayKey)
	if trimmedLogin == "" || trimmedDayKey == "" {
		return
	}

	event := LocationSessionsUpdateEvent{
		Login:  trimmedLogin,
		DayKey: trimmedDayKey,
	}

	locationSessionsUpdateListenersMutex.RLock()
	listeners := append([]func(LocationSessionsUpdateEvent){}, locationSessionsUpdateListeners...)
	locationSessionsUpdateListenersMutex.RUnlock()

	for _, listener := range listeners {
		listener(event)
	}
}
