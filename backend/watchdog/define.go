package watchdog

import (
	"sync"
	"time"
)

var AllUsers map[int]User
var AllUsersMutex sync.Mutex

const (
	NO_BADGE       string = "User didn't badged yet"
	BADGED_ONCE    string = "User badge only once"
	NOT_APPRENTICE string = "User is not an apprentice"

	APPRENTICE_NO_BADGE    string = "Apprentice didn't badged usage yet"
	APPRENTICE_BADGED_ONCE string = "Apprentice badged only once"
	POSTED                 string = "Posted"
	POST_ERROR             string = "Post returned an error"
	POST_OFF               string = "AUTOPOST is off"
)

type User struct {
	ControlAccessID   int           `json:"control_access_id"`
	ControlAccessName string        `json:"control_access_name"`
	Login42           string        `json:"login_42"`
	ID42              string        `json:"id_42"`
	IsApprentice      bool          `json:"is_apprentice"`
	FirstAccess       time.Time     `json:"first_access"`
	LastAccess        time.Time     `json:"last_access"`
	Duration          time.Duration `json:"duration"`
	Profile           ProfileType
	Error             error
	Status            string
}

type ProjectResponse struct {
	Status string `json:"status"`
}

type UserAccess struct {
	UserID      int       `json:"id"`
	FirstAccess time.Time `json:"first_access"`
	LastAccess  time.Time `json:"last_access"`
}

type Property42 struct {
	Login string `json:"ft_login"`
	ID    string `json:"ft_id"`
}

type ProfileType int

const (
	Staff    ProfileType = 1
	Pisciner ProfileType = 2
	Student  ProfileType = 4
)

type UserResponse struct {
	Properties Property42 `json:"properties"`
	Profile    int        `json:"access_profile"`
}

type EventResponse struct {
	Data []Event `json:"data"`
}

type Event struct {
	User     *int      `json:"user"`
	DateTime string    `json:"date_time"`
	Data     EventData `json:"data"`
}

type EventData struct {
	DoorName    string `json:"door_name"`
	UserName    string `json:"user_name"`
	DeviceName  string `json:"device_name"`
	BadgeNumber string `json:"badge_number"`
}
