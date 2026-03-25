package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"watchdog/watchdog"
)

type apiUserState struct {
	ControlAccessID   int        `json:"control_access_id"`
	ControlAccessName string     `json:"control_access_name"`
	Login42           string     `json:"login_42"`
	ID42              string     `json:"id_42"`
	IsApprentice      bool       `json:"is_apprentice"`
	Profile           string     `json:"profile"`
	FirstAccess       *time.Time `json:"first_access,omitempty"`
	LastAccess        *time.Time `json:"last_access,omitempty"`
	DurationSeconds   int64      `json:"duration_seconds"`
	DurationHuman     string     `json:"duration_human"`
	Status            string     `json:"status,omitempty"`
}

type apiStudentMeResponse struct {
	Login   string        `json:"login"`
	Tracked bool          `json:"tracked"`
	User    *apiUserState `json:"user,omitempty"`
}

type apiStudentUpdateRequest struct {
	IsApprentice *bool `json:"is_apprentice,omitempty"`
	Refetch      bool  `json:"refetch,omitempty"`
}

type apiMessageResponse struct {
	Message string `json:"message"`
}

func adminStudentsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, mapUsers(watchdog.SnapshotUsers()))
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func adminStudentHandler(w http.ResponseWriter, r *http.Request) {
	login := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/admin/students/"))
	if login == "" || strings.Contains(login, "/") {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		user, ok := watchdog.FindUserByLogin(login)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "student_not_found", "Student not found in watchdog state.")
			return
		}
		writeJSON(w, http.StatusOK, mapUser(user))
	case http.MethodPatch:
		var req apiStudentUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", "Request body must be valid JSON.")
			return
		}
		if _, ok := watchdog.FindUserByLogin(login); !ok {
			writeJSONError(w, http.StatusNotFound, "student_not_found", "Student not found in watchdog state.")
			return
		}

		switch {
		case req.IsApprentice != nil:
			watchdog.UpdateStudent(login, *req.IsApprentice)
		case req.Refetch:
			watchdog.RefetchStudent(login)
		default:
			writeJSONError(w, http.StatusBadRequest, "invalid_patch", "Provide is_apprentice or refetch=true.")
			return
		}

		user, _ := watchdog.FindUserByLogin(login)
		writeJSON(w, http.StatusOK, mapUser(user))
	case http.MethodDelete:
		user, ok := watchdog.FindUserByLogin(login)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "student_not_found", "Student not found in watchdog state.")
			return
		}
		withPost := true
		if raw := strings.TrimSpace(r.URL.Query().Get("with_post")); raw != "" {
			parsed, err := strconv.ParseBool(raw)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_with_post", "with_post must be a boolean.")
				return
			}
			withPost = parsed
		}
		watchdog.DeleteStudent(login, withPost)
		writeJSON(w, http.StatusOK, apiMessageResponse{Message: "Deleted student " + user.Login42})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func studentMeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authUser := getAuthenticatedUser(r)
	if authUser == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	user, ok := watchdog.FindUserByLogin(authUser.FtLogin)
	if !ok {
		writeJSON(w, http.StatusOK, apiStudentMeResponse{
			Login:   authUser.FtLogin,
			Tracked: false,
		})
		return
	}

	writeJSON(w, http.StatusOK, apiStudentMeResponse{
		Login:   authUser.FtLogin,
		Tracked: true,
		User:    mapUser(user),
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func mapUsers(users []watchdog.User) []apiUserState {
	out := make([]apiUserState, 0, len(users))
	for _, user := range users {
		out = append(out, *mapUser(user))
	}
	return out
}

func mapUser(user watchdog.User) *apiUserState {
	return &apiUserState{
		ControlAccessID:   user.ControlAccessID,
		ControlAccessName: user.ControlAccessName,
		Login42:           user.Login42,
		ID42:              user.ID42,
		IsApprentice:      user.IsApprentice,
		Profile:           profileToString(user.Profile),
		FirstAccess:       timePtr(user.FirstAccess),
		LastAccess:        timePtr(user.LastAccess),
		DurationSeconds:   int64(user.Duration / time.Second),
		DurationHuman:     user.Duration.String(),
		Status:            user.Status,
	}
}

func timePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func profileToString(profile watchdog.ProfileType) string {
	switch profile {
	case watchdog.Staff:
		return "staff"
	case watchdog.Pisciner:
		return "pisciner"
	case watchdog.Student:
		return "student"
	default:
		return "extern"
	}
}
