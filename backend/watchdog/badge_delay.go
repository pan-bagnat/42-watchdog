package watchdog

import (
	"sync"
	"time"
)

type BadgeDelayState struct {
	Known      bool
	Delay      time.Duration
	EventTime  time.Time
	ReceivedAt time.Time
}

var badgeDelayState BadgeDelayState
var badgeDelayStateMutex sync.RWMutex

func UpdateBadgeDelay(eventTime, receivedAt time.Time) BadgeDelayState {
	delay := receivedAt.Sub(eventTime)
	if delay < 0 {
		delay = 0
	}

	state := BadgeDelayState{
		Known:      true,
		Delay:      delay,
		EventTime:  eventTime,
		ReceivedAt: receivedAt,
	}

	badgeDelayStateMutex.Lock()
	badgeDelayState = state
	badgeDelayStateMutex.Unlock()

	return state
}

func SnapshotBadgeDelay() BadgeDelayState {
	badgeDelayStateMutex.RLock()
	defer badgeDelayStateMutex.RUnlock()
	return badgeDelayState
}
