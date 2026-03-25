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
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	http.Handle("/webhook/access-control", verifySignatureMiddleware(http.HandlerFunc(accessControlEndpoint)))
	http.Handle("/commands", requireAdminAuth(http.HandlerFunc(commandHandler)))
	http.Handle("/api/admin/commands", requireAdminAuth(http.HandlerFunc(commandHandler)))
	http.Handle("/api/admin/students", requireAdminAuth(http.HandlerFunc(adminStudentsHandler)))
	http.Handle("/api/admin/students/", requireAdminAuth(http.HandlerFunc(adminStudentHandler)))
	http.Handle("/api/student/me", requireUserAuth(http.HandlerFunc(studentMeHandler)))

	watchdog.Log(fmt.Sprintf("[HTTP] Listening on port %s", port))
	watchdog.Log("[HTTP] ┌─ Available endpoints:")
	watchdog.Log("       ├── /healthz")
	watchdog.Log("       ├── /commands")
	watchdog.Log("       ├── /api/admin/commands")
	watchdog.Log("       ├── /api/admin/students")
	watchdog.Log("       ├── /api/student/me")
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
	watchdog.AllowEvents(true)
	go startHTTPServer("8042")

	// Wait a SIGINT or SIGTERM signal to stop
	sig := <-shutdownSignals
	fmt.Printf("\n") // Used to not display log on the same line as ^C
	watchdog.Log(fmt.Sprintf("Received signal: %v. Starting graceful shutdown...", sig))
	watchdog.PostApprenticesAttendances()
	watchdog.AllowEvents(false)
	watchdog.Log("Watchdog shut down successfully")
	watchdog.Log("")
	watchdog.CloseLogs()
}
