package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
	"watchdog/watchdog"

	"github.com/gorilla/websocket"
)

type wsMessage struct {
	Type              string    `json:"type"`
	Login             string    `json:"login"`
	Day               string    `json:"day,omitempty"`
	Timestamp         time.Time `json:"timestamp,omitempty"`
	DoorName          string    `json:"door_name,omitempty"`
	BadgeDelaySeconds int64     `json:"badge_delay_seconds,omitempty"`
}

type wsHub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

func newWSHub() *wsHub {
	return &wsHub{
		clients: make(map[*websocket.Conn]struct{}),
	}
}

func (hub *wsHub) add(conn *websocket.Conn) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	hub.clients[conn] = struct{}{}
}

func (hub *wsHub) remove(conn *websocket.Conn) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	delete(hub.clients, conn)
}

func (hub *wsHub) broadcast(payload wsMessage) {
	message, err := json.Marshal(payload)
	if err != nil {
		return
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()

	for conn := range hub.clients {
		_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
			_ = conn.Close()
			delete(hub.clients, conn)
		}
	}
}

var liveUpdatesHub = newWSHub()

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return true
		}
		return true
	},
}

func initLiveUpdates() {
	watchdog.RegisterBadgeUpdateListener(func(event watchdog.BadgeUpdateEvent) {
		liveUpdatesHub.broadcast(wsMessage{
			Type:              "badge_received",
			Login:             event.Login,
			Timestamp:         event.Timestamp,
			DoorName:          event.DoorName,
			BadgeDelaySeconds: event.BadgeDelaySeconds,
		})
	})
	watchdog.RegisterLocationSessionsUpdateListener(func(event watchdog.LocationSessionsUpdateEvent) {
		liveUpdatesHub.broadcast(wsMessage{
			Type:  "location_sessions_updated",
			Login: event.Login,
			Day:   event.DayKey,
		})
	})
}

func liveUpdatesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	liveUpdatesHub.add(conn)
	defer func() {
		liveUpdatesHub.remove(conn)
		_ = conn.Close()
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
