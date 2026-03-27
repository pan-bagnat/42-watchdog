package auth

import (
	"backend/core"
	"backend/database"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type contextKey string

const UserCtxKey contextKey = "user"
const PageCtxKey contextKey = "page"

type APIError struct {
	Error   string `json:"error"`             // e.g. "forbidden"
	Code    string `json:"code,omitempty"`    // e.g. "blacklisted"
	Message string `json:"message,omitempty"` // user-friendly text
}

func WriteJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	if code != "" {
		w.Header().Set("X-Error-Code", code) // optional: easy to read from fetch()
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIError{
		Error:   http.StatusText(status),
		Code:    code,
		Message: message,
	})
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sid := core.ReadSessionIDFromCookie(r)
		if sid == "" {
			log.Println("[auth] no session_id")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		session, err := database.GetSession(sid)
		if err != nil {
			// Only clear cookie if session definitely doesn't exist; for transient DB errors keep cookie
			if err == sql.ErrNoRows {
				log.Println("[auth] no such session, clearing cookie")
				core.ClearSessionCookie(w, r.Host)
			} else {
				log.Printf("[auth] session lookup error: %v", err)
			}
			go database.PurgeExpiredSessions()
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if session.ExpiresAt.Before(time.Now()) {
			log.Println("[auth] expired session")
			go database.PurgeExpiredSessions()
			core.ClearSessionCookie(w, r.Host)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		user, err := core.GetUser(session.Login)
		if err != nil {
			log.Println("[auth] user not found for session:", err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		log.Printf("[auth] user %s authenticated via session", user.FtLogin)

		if time.Since(user.LastSeen) > time.Minute {
			go core.TouchUserLastSeen(user.FtLogin)
		}

		ctx := context.WithValue(r.Context(), UserCtxKey, &user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func AdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := r.Context().Value(UserCtxKey).(*core.User)
		if !ok || u == nil {
			// Not authenticated → 401 so the SPA can redirect to /login
			WriteJSONError(w, http.StatusUnauthorized, "unauthorized", "Please sign in.")
			return
		}

		if !isAdminUser(u) {
			WriteJSONError(w, http.StatusForbidden, "admin_required", "You are not allowed to view this content.")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isAdminUser(u *core.User) bool {
	if u == nil {
		return false
	}
	return u.IsStaff
}

func BlackListMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := r.Context().Value(UserCtxKey).(*core.User)
		if !ok {
			fmt.Printf("Couldn't get user\n")
			http.Redirect(w, r, "/", http.StatusForbidden)
			return
		}
		if !u.IsBlacklisted {
			next.ServeHTTP(w, r)
			return
		}

		n, err := database.DeleteUserSessions(r.Context(), u.ID)
		if err != nil {
			log.Printf("couldn't delete user %s sessions: %s\n", u.FtLogin, err.Error())
		} else {
			log.Printf("deleted %d sessions for user %s\n", n, u.FtLogin)
		}
		core.ClearSessionCookie(w, r.Host)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		WriteJSONError(w, http.StatusForbidden, "blacklisted", "Your account is currently blacklisted. Contact your bocal.")
		fmt.Printf("[auth] user %s is blacklisted, returned 403 Forbidden\n", u.FtLogin)
	})
}
