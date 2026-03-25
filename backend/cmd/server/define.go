package main

import (
	"os"
	"strings"
)

func getWebhookSecret() string {
	return strings.TrimSpace(os.Getenv("WEBHOOK_SECRET"))
}

// Represents the inner "data" object within the main "data" object
type CAInnerData struct {
	DeviceName  string `json:"device_name"`
	DoorName    string `json:"door_name"`
	BadgeNumber string `json:"badge_number"` // Keep as string, even if numbers, as it's quoted ""
	UserName    string `json:"user_name"`
	EntryNumber *int   `json:"entry_number"` // Use pointer for nullable integer
}

// Represents the main "data" object in the webhook payload
type CAEventData struct {
	URL      string      `json:"url"`
	Code     int         `json:"code"`
	Name     string      `json:"name"` // Handles unicode like "Acc\u00e8s autoris\u00e9"
	Level    string      `json:"level"`
	DateTime string      `json:"date_time"` // Consider parsing to time.Time after unmarshalling
	User     *int        `json:"user"`      // Use pointer for nullable integer
	Company  string      `json:"company"`
	Device   string      `json:"device"`
	Badge    *int        `json:"badge"` // Use pointer for nullable integer (even if it looks like a string in one payload, it's a number in the second)
	Event    CAInnerData `json:"data"`  // Nested struct
}

// Represents the overall webhook payload structure
type CAPayload struct {
	IboxIP   string      `json:"ibox_ip"`
	IboxSN   string      `json:"ibox_sn"`
	DateTime string      `json:"datetime"` // Consider parsing to time.Time after unmarshalling
	Category string      `json:"category"`
	Action   string      `json:"action"`
	Operator any         `json:"operator"` // Use any for unknown or potentially varying null types
	Data     CAEventData `json:"data"`
}

type CommandPayload struct {
	Command string `json:"ibox_ip"`
}

type CommandRequest struct {
	Command    string         `json:"command"`
	Parameters map[string]any `json:"parameters,omitempty"` // Use a map for flexible parameters
}
