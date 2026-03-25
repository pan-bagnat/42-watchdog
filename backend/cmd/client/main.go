package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

type CommandRequest struct {
	Command    string         `json:"command"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

var serverURL string
var authorizationHeader string
var cookieHeader string
var sessionIDHeader string

func sendCommand(command string, params map[string]any) {
	cmdReq := CommandRequest{
		Command:    command,
		Parameters: params,
	}

	jsonBody, err := json.Marshal(cmdReq)
	if err != nil {
		fmt.Println("JSON encode error:", err)
		os.Exit(1)
	}

	req, err := http.NewRequest(http.MethodPost, serverURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		fmt.Println("Request build error:", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")
	if authorizationHeader != "" {
		req.Header.Set("Authorization", authorizationHeader)
	}
	if cookieHeader != "" {
		req.Header.Set("Cookie", cookieHeader)
	}
	if sessionIDHeader != "" {
		req.Header.Set("X-Session-Id", sessionIDHeader)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("Request error:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	fmt.Printf("Status: %s\n", resp.Status)

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Response: %s\n", bodyBytes)
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "watchdog-client",
		Short: "Client for sending commands to the watchdog server",
	}

	rootCmd.PersistentFlags().StringVarP(&serverURL, "url", "u", "http://localhost:8042/commands", "Full URL of the server endpoint")
	rootCmd.PersistentFlags().StringVar(&authorizationHeader, "authorization", strings.TrimSpace(os.Getenv("WATCHDOG_AUTHORIZATION")), "Authorization header to forward to the server")
	rootCmd.PersistentFlags().StringVar(&cookieHeader, "cookie", strings.TrimSpace(os.Getenv("WATCHDOG_COOKIE")), "Cookie header to forward to the server")
	rootCmd.PersistentFlags().StringVar(&sessionIDHeader, "session-id", strings.TrimSpace(os.Getenv("WATCHDOG_SESSION_ID")), "X-Session-Id header to forward to the server")

	rootCmd.AddCommand(&cobra.Command{
		Use:   "start",
		Short: "Send start_listen command",
		Run: func(cmd *cobra.Command, args []string) {
			sendCommand("start_listen", nil)
		},
	})

	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Send stop_listen command",
		Run: func(cmd *cobra.Command, args []string) {
			postAttendance, _ := cmd.Flags().GetBool("post-attendance")
			params := map[string]any{}
			if postAttendance {
				params["post_attendance"] = true
			}
			sendCommand("stop_listen", params)
		},
	}
	stopCmd.Flags().Bool("post-attendance", false, "Post attendance after stopping")
	rootCmd.AddCommand(stopCmd)

	rootCmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Send get_status command",
		Run: func(cmd *cobra.Command, args []string) {
			sendCommand("get_status", nil)
		},
	})

	rootCmd.AddCommand(&cobra.Command{
		Use:   "notify",
		Short: "Send notify_students command",
		Run: func(cmd *cobra.Command, args []string) {
			sendCommand("notify_students", nil)
		},
	})

	updateStudentCmd := &cobra.Command{
		Use:   "update-student",
		Short: "Refetch or force apprentice status of students",
		Run: func(cmd *cobra.Command, args []string) {
			login, _ := cmd.Flags().GetString("login")
			isApprenticeFlag := cmd.Flags().Changed("is-apprentice")
			isApprentice, _ := cmd.Flags().GetBool("is-apprentice")

			params := map[string]any{}

			if login != "" {
				params["login"] = login
			}
			if isApprenticeFlag {
				params["is_alternant"] = isApprentice
			}

			sendCommand("update_student_status", params)
		},
	}
	updateStudentCmd.Flags().String("login", "", "Login of the student")
	updateStudentCmd.Flags().Bool("is-apprentice", false, "Force apprentice status")
	rootCmd.AddCommand(updateStudentCmd)

	deleteStudentCmd := &cobra.Command{
		Use:   "delete-student",
		Short: "Delete a student from watchdog server",
		Run: func(cmd *cobra.Command, args []string) {
			login, _ := cmd.Flags().GetString("login")
			withPost, _ := cmd.Flags().GetBool("withPost")

			params := map[string]any{
				"withPost": withPost,
			}

			if login != "" {
				params["login"] = login
			}

			sendCommand("delete_student", params)
		},
	}

	deleteStudentCmd.Flags().String("login", "", "Login of the student")
	deleteStudentCmd.Flags().Bool("withPost", true, "Whether to post attendance before deleting")
	deleteStudentCmd.MarkFlagRequired("login")
	rootCmd.AddCommand(deleteStudentCmd)

	deletePiscinerCmd := &cobra.Command{
		Use:   "delete-all-pisciner",
		Short: "Delete all pisciner from watchdog server",
		Run: func(cmd *cobra.Command, args []string) {
			sendCommand("delete_all_pisciner", nil)
		},
	}
	rootCmd.AddCommand(deletePiscinerCmd)

	rootCmd.AddCommand(&cobra.Command{
		Use:    "completion",
		Short:  "Generate shell completion script",
		Hidden: true,
		Long: `To load completions:
	
	Bash:
	
	  source <(watchdog-client completion bash)
	
	  # To load completions for each session, execute once:
	  # Linux:
	  watchdog-client completion bash > /etc/bash_completion.d/watchdog-client
	  # macOS:
	  watchdog-client completion bash > /usr/local/etc/bash_completion.d/watchdog-client
	
	Zsh:
	
	  echo "autoload -U compinit; compinit" >> ~/.zshrc
	  watchdog-client completion zsh > "${fpath[1]}/_watchdog-client"
	`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) < 1 {
				fmt.Println("Shell type required: bash, zsh, fish...")
				return
			}
			switch args[0] {
			case "bash":
				rootCmd.GenBashCompletion(os.Stdout)
			case "zsh":
				rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				rootCmd.GenFishCompletion(os.Stdout, true)
			default:
				fmt.Println("Unsupported shell:", args[0])
			}
		},
	})

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
