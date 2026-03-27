package core

import (
	"os"
	"strings"
	"sync"
)

var (
	adminLoginOnce sync.Once
	adminLoginsSet map[string]struct{}
)

func loadAdminLogins() {
	adminLoginsSet = make(map[string]struct{})
	raw := strings.TrimSpace(os.Getenv("ADMIN_LOGINS"))
	if raw == "" {
		return
	}
	for _, part := range strings.Split(raw, ",") {
		login := strings.ToLower(strings.TrimSpace(part))
		if login == "" {
			continue
		}
		adminLoginsSet[login] = struct{}{}
	}
}

func isLoginInAdminList(ftLogin string) bool {
	adminLoginOnce.Do(loadAdminLogins)
	login := strings.ToLower(strings.TrimSpace(ftLogin))
	_, ok := adminLoginsSet[login]
	return ok
}
