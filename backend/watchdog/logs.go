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

func CloseLogs() {
	logFile.Close()
}
