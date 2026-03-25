package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v2"
)

const AccessControl string = "access-control"
const FTv2 string = "42-v2"
const FTAttendance string = "42-attendance"

var ConfigData ConfigFile

type ConfigWatchtime struct {
	Monday    [][]string `yaml:"monday"`
	Tuesday   [][]string `yaml:"tuesday"`
	Wednesday [][]string `yaml:"wednesday"`
	Thursday  [][]string `yaml:"thursday"`
	Friday    [][]string `yaml:"friday"`
	Saturday  [][]string `yaml:"saturday"`
	Sunday    [][]string `yaml:"sunday"`
}

type ConfigAccessControl struct {
	Endpoint string `yaml:"endpoint"`
	TestPath string `yaml:"testpath"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type ConfigAPIV2 struct {
	TokenUrl           string   `yaml:"tokenUrl"`
	Endpoint           string   `yaml:"endpoint"`
	TestPath           string   `yaml:"testpath"`
	Uid                string   `yaml:"uid"`
	Secret             string   `yaml:"secret"`
	Scope              string   `yaml:"scope"`
	CampusID           string   `yaml:"campusId"`
	ApprenticeProjects []string `yaml:"apprenticeProjects"`
}

type ConfigAttendance42 struct {
	AutoPost bool   `yaml:"autoPost"`
	TokenUrl string `yaml:"tokenUrl"`
	Endpoint string `yaml:"endpoint"`
	TestPath string `yaml:"testpath"`
	Uid      string `yaml:"uid"`
	Secret   string `yaml:"secret"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type ConfigMailer struct {
	SmtpServer string   `yaml:"smtp_server"`
	SmtpPort   int      `yaml:"smtp_port"`
	SmtpAuth   bool     `yaml:"smtp_auth"`
	SmtpUser   string   `yaml:"smtp_user"`
	SmtpPass   string   `yaml:"smtp_pass"`
	SmtpTLS    bool     `yaml:"smtp_tls"`
	Helo       string   `yaml:"helo"`
	FromName   string   `yaml:"from_name"`
	FromMail   string   `yaml:"from_mail"`
	Recipients []string `yaml:"recipients"`
}

type ConfigFile struct {
	AccessControl ConfigAccessControl `yaml:"AccessControl"`
	ApiV2         ConfigAPIV2         `yaml:"42apiV2"`
	Attendance42  ConfigAttendance42  `yaml:"42Attendance"`
	Mailer        ConfigMailer        `yaml:"mailer"`
	Watchtime     ConfigWatchtime     `yaml:"watchtime"`
}

func LoadConfig(path string) error {
	if err := loadEnvFiles(path); err != nil {
		return err
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	err = decoder.Decode(&ConfigData)
	if err != nil {
		return err
	}

	if err := applyEnvOverrides(); err != nil {
		return err
	}
	return nil
}

func loadEnvFiles(configPath string) error {
	paths := []string{}

	if envPath := strings.TrimSpace(os.Getenv("WATCHDOG_ENV_FILE")); envPath != "" {
		paths = append(paths, envPath)
	} else {
		paths = append(paths, ".env")
		if configPath != "" {
			paths = append(paths, filepath.Join(filepath.Dir(configPath), ".env"))
		}
	}

	existing := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		cleanPath := filepath.Clean(path)
		if _, ok := seen[cleanPath]; ok {
			continue
		}
		seen[cleanPath] = struct{}{}
		if _, err := os.Stat(cleanPath); err == nil {
			existing = append(existing, cleanPath)
		}
	}

	if len(existing) == 0 {
		return nil
	}
	return godotenv.Overload(existing...)
}

func applyEnvOverrides() error {
	ConfigData.AccessControl.Endpoint = getEnv("ACCESS_CONTROL_ENDPOINT", ConfigData.AccessControl.Endpoint)
	ConfigData.AccessControl.TestPath = getEnv("ACCESS_CONTROL_TESTPATH", ConfigData.AccessControl.TestPath)
	ConfigData.AccessControl.Username = getEnv("ACCESS_CONTROL_USERNAME", ConfigData.AccessControl.Username)
	ConfigData.AccessControl.Password = getEnv("ACCESS_CONTROL_PASSWORD", ConfigData.AccessControl.Password)

	ConfigData.ApiV2.TokenUrl = getEnv("FT_V2_TOKEN_URL", ConfigData.ApiV2.TokenUrl)
	ConfigData.ApiV2.Endpoint = getEnv("FT_V2_ENDPOINT", ConfigData.ApiV2.Endpoint)
	ConfigData.ApiV2.TestPath = getEnv("FT_V2_TESTPATH", ConfigData.ApiV2.TestPath)
	ConfigData.ApiV2.Uid = getEnv("FT_V2_UID", ConfigData.ApiV2.Uid)
	ConfigData.ApiV2.Secret = getEnv("FT_V2_SECRET", ConfigData.ApiV2.Secret)
	ConfigData.ApiV2.Scope = getEnv("FT_V2_SCOPE", ConfigData.ApiV2.Scope)
	ConfigData.ApiV2.CampusID = getEnv("FT_V2_CAMPUS_ID", ConfigData.ApiV2.CampusID)
	ConfigData.ApiV2.ApprenticeProjects = getEnvList("FT_V2_APPRENTICE_PROJECTS", ConfigData.ApiV2.ApprenticeProjects)

	var err error
	ConfigData.Attendance42.AutoPost, err = getEnvBool("FT_ATTENDANCE_AUTO_POST", ConfigData.Attendance42.AutoPost)
	if err != nil {
		return err
	}
	ConfigData.Attendance42.TokenUrl = getEnv("FT_ATTENDANCE_TOKEN_URL", ConfigData.Attendance42.TokenUrl)
	ConfigData.Attendance42.Endpoint = getEnv("FT_ATTENDANCE_ENDPOINT", ConfigData.Attendance42.Endpoint)
	ConfigData.Attendance42.TestPath = getEnv("FT_ATTENDANCE_TESTPATH", ConfigData.Attendance42.TestPath)
	ConfigData.Attendance42.Uid = getEnv("FT_ATTENDANCE_UID", ConfigData.Attendance42.Uid)
	ConfigData.Attendance42.Secret = getEnv("FT_ATTENDANCE_SECRET", ConfigData.Attendance42.Secret)
	ConfigData.Attendance42.Username = getEnv("FT_ATTENDANCE_USERNAME", ConfigData.Attendance42.Username)
	ConfigData.Attendance42.Password = getEnv("FT_ATTENDANCE_PASSWORD", ConfigData.Attendance42.Password)

	ConfigData.Mailer.SmtpServer = getEnv("MAILER_SMTP_SERVER", ConfigData.Mailer.SmtpServer)
	ConfigData.Mailer.SmtpPort, err = getEnvInt("MAILER_SMTP_PORT", ConfigData.Mailer.SmtpPort)
	if err != nil {
		return err
	}
	ConfigData.Mailer.SmtpAuth, err = getEnvBool("MAILER_SMTP_AUTH", ConfigData.Mailer.SmtpAuth)
	if err != nil {
		return err
	}
	ConfigData.Mailer.SmtpUser = getEnv("MAILER_SMTP_USER", ConfigData.Mailer.SmtpUser)
	ConfigData.Mailer.SmtpPass = getEnv("MAILER_SMTP_PASS", ConfigData.Mailer.SmtpPass)
	ConfigData.Mailer.SmtpTLS, err = getEnvBool("MAILER_SMTP_TLS", ConfigData.Mailer.SmtpTLS)
	if err != nil {
		return err
	}
	ConfigData.Mailer.Helo = getEnv("MAILER_HELO", ConfigData.Mailer.Helo)
	ConfigData.Mailer.FromName = getEnv("MAILER_FROM_NAME", ConfigData.Mailer.FromName)
	ConfigData.Mailer.FromMail = getEnv("MAILER_FROM_MAIL", ConfigData.Mailer.FromMail)
	ConfigData.Mailer.Recipients = getEnvList("MAILER_RECIPIENTS", ConfigData.Mailer.Recipients)

	return nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func getEnvBool(key string, fallback bool) (bool, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return fallback, fmt.Errorf("invalid boolean value for %s: %w", key, err)
	}
	return parsed, nil
}

func getEnvInt(key string, fallback int) (int, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback, fmt.Errorf("invalid integer value for %s: %w", key, err)
	}
	return parsed, nil
}

func getEnvList(key string, fallback []string) []string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	items := strings.Split(value, ",")
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
