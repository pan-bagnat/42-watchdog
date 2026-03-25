package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
	"watchdog/watchdog"
)

// Handler for the /command endpoint
func commandHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var cmdReq CommandRequest
	err = json.Unmarshal(bodyBytes, &cmdReq)
	if err != nil {
		http.Error(w, "Invalid command format (expecting JSON with 'command' field)", http.StatusBadRequest)
		return
	}

	if user := getAuthenticatedUser(r); user != nil {
		watchdog.Log(fmt.Sprintf("[CLI] 🛠️  Received command: %s (remote user: %s)", cmdReq.Command, user.FtLogin))
	} else {
		watchdog.Log(fmt.Sprintf("[CLI] 🛠️  Received command: %s", cmdReq.Command))
	}
	responseMessage := ""
	statusCode := http.StatusOK
	// Process the command
	switch cmdReq.Command {
	case "start_listen":
		watchdog.AllowEvents(true)
		responseMessage = "Enabled listening hooks (Check server logs for more details)"
	case "stop_listen":
		shouldPost := false
		if params := cmdReq.Parameters; params != nil {
			if postVal, ok := params["post_attendance"]; ok {
				if postBool, ok := postVal.(bool); ok {
					shouldPost = postBool
					watchdog.Log(fmt.Sprintf("[CLI]       With argument post_attendance: %t", shouldPost))
				}
			}
		}
		watchdog.AllowEvents(false)
		if shouldPost {
			watchdog.PostApprenticesAttendances()
			responseMessage = "Disabled listening hooks and posted attendances (Check server logs for more details)"
		} else {
			responseMessage = "Disabled listening hooks (Check server logs for more details)"
		}
	case "update_student_status":
		params := cmdReq.Parameters
		login, hasLogin := "", false
		isAlternant, hasIsAlternant := false, false

		if params != nil {
			if l, ok := params["login"].(string); ok {
				login = l
				hasLogin = true
			}
			if alt, ok := params["is_alternant"].(bool); ok {
				isAlternant = alt
				hasIsAlternant = true
			}
		}

		switch {
		case !hasLogin:
			watchdog.Log("[CLI] 🔁 Refetching all student's alternance status")
			watchdog.RefetchAllStudents()
			responseMessage = "Triggered full alternant status refresh"

		case hasLogin && !hasIsAlternant:
			watchdog.Log(fmt.Sprintf("[CLI] 🔄 Refetching status for student: %s", login))
			watchdog.RefetchStudent(login)
			responseMessage = fmt.Sprintf("Refetched status for %s", login)

		case hasLogin && hasIsAlternant:
			watchdog.Log(fmt.Sprintf("[CLI] 🔧 Forcing status of %s to alternant=%t", login, isAlternant))
			watchdog.UpdateStudent(login, isAlternant)
			responseMessage = fmt.Sprintf("Forced status for %s to alternant=%t", login, isAlternant)

		default:
			responseMessage = "Invalid parameters for update_student_status"
			statusCode = http.StatusBadRequest
		}

	case "delete_student":
		params := cmdReq.Parameters
		login, hasLogin := "", false
		withPost := true // Default value

		if params != nil {
			if l, ok := params["login"].(string); ok {
				login = l
				hasLogin = true
			}
			if wp, ok := params["withPost"].(bool); ok {
				withPost = wp
			}
		}

		switch {
		case hasLogin:
			watchdog.DeleteStudent(login, withPost)
			responseMessage = fmt.Sprintf("Deleted student %s", login)

		default:
			responseMessage = "You must provide a login to delete"
			statusCode = http.StatusBadRequest
		}

	case "delete_all_pisciner":
		watchdog.DeleteAllPisciners()
	case "get_status":
		watchdog.PrintUsersTimers()
		responseMessage = "Check server logs for status detail"
	case "notify_students":
		statusCode = http.StatusNotImplemented
		responseMessage = "Coming soon"
	default:
		responseMessage = fmt.Sprintf("Unknown command: %s", cmdReq.Command)
		statusCode = http.StatusBadRequest
	}

	// Send response
	w.WriteHeader(statusCode)
	fmt.Fprint(w, responseMessage)
}

// Middleware function to verify the webhook signature
func verifySignatureMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			watchdog.Log(fmt.Sprintf("Middleware: Method not allowed: %s", r.Method))
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		receivedSigHex := r.Header.Get("x-webhook-signature")
		if receivedSigHex == "" {
			watchdog.Log("Middleware: Missing x-webhook-signature header")
			http.Error(w, "Missing signature header", http.StatusUnauthorized)
			return
		}
		webhookSecret := getWebhookSecret()
		if webhookSecret == "" {
			watchdog.Log("Middleware: WEBHOOK_SECRET is not configured")
			http.Error(w, "Webhook secret is not configured", http.StatusServiceUnavailable)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			watchdog.Log(fmt.Sprintf("Middleware: Error reading request body: %v", err))
			http.Error(w, "Error reading request body", http.StatusInternalServerError)
			return
		}

		// Restore the body so the next handler can read it.
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		mac := hmac.New(sha512.New, []byte(webhookSecret))
		mac.Write(bodyBytes)
		expectedSigBytes := mac.Sum(nil)
		calculatedSigHex := hex.EncodeToString(expectedSigBytes)

		if !hmac.Equal([]byte(calculatedSigHex), []byte(receivedSigHex)) {
			watchdog.Log("Middleware: Invalid signature. Request rejected")
			http.Error(w, "Invalid signature", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func accessControlEndpoint(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Handler: Error reading restored request body: %v", err)
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var payload CAPayload
	err = json.Unmarshal(bodyBytes, &payload)
	if err != nil {
		log.Printf("Handler: Error unmarshalling webhook JSON: %v", err)
		http.Error(w, "Invalid payload format", http.StatusBadRequest)
		return
	}

	// Only allow events that are "Access Granted" types
	if payload.Data.Code != 48 {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Webhook received, event ignored (code != 48)")
		return
	}

	if payload.Data.User == nil {
		log.Printf("Handler: User ID is null")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Webhook received with empty user")
		return
	}
	layout := "2006-01-02 15:04:05"
	loc, _ := time.LoadLocation("Europe/Paris")
	eventTime, err := time.ParseInLocation(layout, payload.Data.DateTime, loc)
	if err != nil {
		log.Printf("Handler: Error parsing event time '%s': %v", payload.Data.DateTime, err)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Webhook couldn't parse event time")
	}
	go watchdog.UpdateUserAccess(*payload.Data.User, payload.Data.Event.UserName, eventTime, payload.Data.Event.DoorName)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Webhook received and queued to process")
}
