package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"watchdog/config"
	"watchdog/watchdog"
)

func startHTTPServer(port string) {
	initLiveUpdates()

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	http.HandleFunc("/auth/42/login", authLoginHandler)
	http.HandleFunc("/auth/42/callback", authCallbackHandler)
	http.HandleFunc("/auth/logout", authLogoutHandler)
	http.Handle("/api/auth/me", requireUserAuth(http.HandlerFunc(authMeHandler)))
	http.Handle("/webhook/access-control", verifySignatureMiddleware(http.HandlerFunc(accessControlEndpoint)))
	http.Handle("/commands", requireAdminAuth(http.HandlerFunc(commandHandler)))
	http.Handle("/api/admin/commands", requireAdminAuth(http.HandlerFunc(commandHandler)))
	http.Handle("/api/admin/calendar", requireAdminAuth(http.HandlerFunc(adminCalendarHandler)))
	http.Handle("/api/admin/student-days/", requireAdminAuth(http.HandlerFunc(adminStudentDaysHandler)))
	http.Handle("/api/admin/users", requireAdminAuth(http.HandlerFunc(adminUsersHandler)))
	http.Handle("/api/admin/users/", requireAdminAuth(http.HandlerFunc(adminUserDetailHandler)))
	http.Handle("/api/admin/students", requireAdminAuth(http.HandlerFunc(adminStudentsHandler)))
	http.Handle("/api/admin/students/", requireAdminAuth(http.HandlerFunc(adminStudentHandler)))
	http.Handle("/api/student/detail", requireUserAuth(http.HandlerFunc(studentDetailHandler)))
	http.Handle("/api/student/me", requireUserAuth(http.HandlerFunc(studentMeHandler)))
	http.Handle("/api/live", requireUserAuth(http.HandlerFunc(liveUpdatesHandler)))

	watchdog.Log(fmt.Sprintf("[HTTP] Listening on port %s", port))
	watchdog.Log("[HTTP] ┌─ Available endpoints:")
	watchdog.Log("       ├── /healthz")
	watchdog.Log("       ├── /auth/42/login")
	watchdog.Log("       ├── /auth/42/callback")
	watchdog.Log("       ├── /auth/logout")
	watchdog.Log("       ├── /api/auth/me")
	watchdog.Log("       ├── /commands")
	watchdog.Log("       ├── /api/admin/commands")
	watchdog.Log("       ├── /api/admin/calendar")
	watchdog.Log("       ├── /api/admin/student-days/{login}")
	watchdog.Log("       ├── /api/admin/users")
	watchdog.Log("       ├── /api/admin/users/{login}")
	watchdog.Log("       ├── /api/admin/students")
	watchdog.Log("       ├── /api/student/detail")
	watchdog.Log("       ├── /api/student/me")
	watchdog.Log("       ├── /api/live")
	watchdog.Log("       └── /webhook/access-control")

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		watchdog.Log(fmt.Sprintf("[HTTP] [FATAL] could not start server: %s\n", err))
		os.Exit(1)
	}
}

func main() {
	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, syscall.SIGINT, syscall.SIGTERM)

	if len(os.Args) <= 2 {
		fmt.Printf("Invalid program usage:\n")
		fmt.Printf("./watchdog <path_to_config_file> <path_to_log_file>\n")
		os.Exit(1)
	}

	configFile := os.Args[1]
	logFile := os.Args[2]

	info, err := os.Stat(logFile)
	if err == nil && info.IsDir() {
		fmt.Printf("path '%s' must be a file, not a directory\n", logFile)
		os.Exit(1)
	}

	err = watchdog.InitLogs(logFile)
	if err != nil {
		fmt.Printf("ERROR: couldn't init logs")
		os.Exit(1)
	}
	watchdog.Log(fmt.Sprintf("[WATCHDOG] 📝 Initialiazed log file %s", logFile))
	watchdog.Log(fmt.Sprintf("[WATCHDOG] 💾 Loading config using file %s", configFile))
	err = config.LoadConfig(configFile)
	if err != nil {
		watchdog.Log(fmt.Sprintf("[WATCHDOG] ERROR: couldn't load config: %s", err.Error()))
		os.Exit(1)
	}
	err = watchdog.InitAPIs()
	if err != nil {
		fmt.Printf("Error: %s\n", err.Error())
		return
	}
	err = watchdog.InitStorage()
	if err != nil {
		watchdog.Log(fmt.Sprintf("[WATCHDOG] ERROR: couldn't initialize persistent storage: %s", err.Error()))
		os.Exit(1)
	}
	watchdog.AllowEvents(true)
	go startHTTPServer("8042")

	// Wait a SIGINT or SIGTERM signal to stop
	sig := <-shutdownSignals
	fmt.Printf("\n") // Used to not display log on the same line as ^C
	watchdog.Log(fmt.Sprintf("Received signal: %v. Starting graceful shutdown...", sig))
	watchdog.PostApprenticesAttendances()
	watchdog.AllowEvents(false)
	watchdog.CloseStorage()
	watchdog.Log("Watchdog shut down successfully")
	watchdog.Log("")
	watchdog.CloseLogs()
}
