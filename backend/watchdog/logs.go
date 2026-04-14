package watchdog

import (
	"fmt"
	"os"
	"strings"
	"time"
)

var logFile *os.File

func InitLogs(logPath string) (err error) {
	logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return err
	}
	return nil
}

func Log(msg string) {
	if msg == "" {
		fmt.Fprintf(logFile, "\n")
		fmt.Printf("\n")
	} else {
		msg = strings.TrimRight(msg, "\n")
		fmt.Fprintf(logFile, "[%s] %s\n", time.Now().Format("02/01/2006 - 15:04:05 MST"), msg)
		fmt.Printf("[%s] %s\n", time.Now().Format("02/01/2006 - 15:04:05 MST"), msg)
	}
}

func Trace(scope string, format string, args ...any) {
	scope = strings.ToUpper(strings.TrimSpace(scope))
	if scope == "" {
		scope = "TRACE"
	}
	Log(fmt.Sprintf("[WATCHDOG] [%s] %s", scope, fmt.Sprintf(format, args...)))
}

func traceTime(ts time.Time) string {
	if ts.IsZero() {
		return "<zero>"
	}
	return ts.In(parisLocation()).Format(time.RFC3339Nano)
}

func traceBounds(beginAt, endAt time.Time) string {
	return fmt.Sprintf("%s -> %s", traceTime(beginAt), traceTime(endAt))
}

func CloseLogs() {
	logFile.Close()
}
