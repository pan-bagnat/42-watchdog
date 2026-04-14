package watchdog

import (
	"strings"
	"sync"
)

type LocationMonthUpdateEvent struct {
	Login    string `json:"login"`
	MonthKey string `json:"month"`
}

var locationMonthUpdateListeners []func(LocationMonthUpdateEvent)
var locationMonthUpdateListenersMutex sync.RWMutex

func RegisterLocationMonthUpdateListener(listener func(LocationMonthUpdateEvent)) {
	if listener == nil {
		return
	}

	locationMonthUpdateListenersMutex.Lock()
	defer locationMonthUpdateListenersMutex.Unlock()

	locationMonthUpdateListeners = append(locationMonthUpdateListeners, listener)
}

func NotifyLocationMonthUpdate(login, monthKey string) {
	trimmedLogin := strings.ToLower(strings.TrimSpace(login))
	trimmedMonthKey := strings.TrimSpace(monthKey)
	if trimmedLogin == "" || trimmedMonthKey == "" {
		return
	}

	event := LocationMonthUpdateEvent{
		Login:    trimmedLogin,
		MonthKey: trimmedMonthKey,
	}

	locationMonthUpdateListenersMutex.RLock()
	listeners := append([]func(LocationMonthUpdateEvent){}, locationMonthUpdateListeners...)
	locationMonthUpdateListenersMutex.RUnlock()

	for _, listener := range listeners {
		listener(event)
	}
}
