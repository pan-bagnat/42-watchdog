package database

import (
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"

	_ "github.com/lib/pq"
)

var mainDB *sqlx.DB
var testDB *sqlx.DB

func init() {
	var err error
	connStr := os.Getenv("AUTH_POSTGRES_URL")
	if connStr == "" {
		connStr = os.Getenv("POSTGRES_URL")
	}
	if connStr == "" {
		log.Fatal("AUTH_POSTGRES_URL/POSTGRES_URL is not set")
	}
	mainDB, err = sqlx.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Couldn't connect to database: ", err)
	}
}

func formatUsers(us []User) string {
	var b strings.Builder
	if us == nil {
		return "nil"
	}
	b.WriteString("[]User{\n")
	for _, u := range us {
		y, mon, d := u.LastSeen.Date()
		h, min, sec := u.LastSeen.Clock()
		b.WriteString(fmt.Sprintf(
			"\t{ID: %q, FtLogin: %q, FtID: %q, FtIsStaff: %t, PhotoURL: %q, LastSeen: time.Date(%d, %d, %d, %d, %d, %d, 0, time.UTC)},\n",
			u.ID, u.FtLogin, u.FtID, u.FtIsStaff, u.PhotoURL,
			y, int(mon), d, h, min, sec,
		))
	}
	b.WriteString("},")
	return b.String()
}

func DropDatabase(t *testing.T, dbName string) {
	_, err := mainDB.Exec(fmt.Sprintf("DROP DATABASE %s WITH (FORCE)", dbName))
	if err != nil {
		t.Fatalf("failed to drop database %s: %v", dbName, err)
	}
}

func CreateAndPopulateDatabase(t *testing.T, dbName string, sqlFile string) *sqlx.DB {
	_, err := mainDB.Exec(fmt.Sprintf(
		"DROP DATABASE IF EXISTS %s WITH (FORCE)",
		dbName,
	))
	if err != nil {
		t.Fatalf("failed to drop test database %s: %v", dbName, err)
	}

	_, err = mainDB.Exec(fmt.Sprintf(
		"CREATE DATABASE %s TEMPLATE schema_template",
		dbName,
	))
	if err != nil {
		t.Fatalf("failed to create test database %s: %v", dbName, err)
	}

	host := os.Getenv("AUTH_POSTGRES_HOST")
	if host == "" {
		host = os.Getenv("POSTGRES_HOST")
	}
	if host == "" {
		host = "localhost"
	}

	port := os.Getenv("AUTH_POSTGRES_PORT")
	if port == "" {
		port = os.Getenv("POSTGRES_PORT")
	}
	if port == "" {
		port = "localhost"
	}

	dbPassword := os.Getenv("AUTH_POSTGRES_PASSWORD")
	if dbPassword == "" {
		dbPassword = os.Getenv("POSTGRES_PASSWORD")
	}
	if dbPassword == "" {
		dbPassword = "uh oh..."
	}

	dbUser := os.Getenv("AUTH_POSTGRES_USER")
	if dbUser == "" {
		dbUser = os.Getenv("POSTGRES_USER")
	}
	if dbUser == "" {
		dbUser = "uh oh..."
	}

	testDB, err = sqlx.Open("postgres", fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=disable", dbUser, dbPassword, host, port, dbName))
	if err != nil {
		t.Fatalf("failed to connect to test database %s: %v", dbName, err)
	}

	_, err = testDB.Exec(sqlFile)
	if err != nil {
		t.Fatalf("failed to execute SQL in %s: %v", sqlFile, err)
	}

	tmp := mainDB
	mainDB = testDB
	testDB = tmp
	t.Cleanup(func() {
		tmp := mainDB
		mainDB = testDB
		testDB = tmp
		DropDatabase(t, dbName)
	})

	return mainDB
}
