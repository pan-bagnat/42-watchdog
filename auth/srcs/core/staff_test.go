package core

import (
	"sync"
	"testing"
)

func resetAdminLoginCacheForTest() {
	adminLoginOnce = sync.Once{}
	adminLoginsSet = nil
}

func TestIsLoginInAdminList_WithAdminLogins(t *testing.T) {
	t.Setenv("ADMIN_LOGINS", "heinz, alice")
	resetAdminLoginCacheForTest()

	if !isLoginInAdminList("heinz") {
		t.Fatalf("expected heinz to be staff from ADMIN_LOGINS")
	}
	if isLoginInAdminList("bob") {
		t.Fatalf("expected bob to not be in ADMIN_LOGINS")
	}
}

func TestIsLoginInAdminList_EmptyList(t *testing.T) {
	t.Setenv("ADMIN_LOGINS", "")
	resetAdminLoginCacheForTest()

	if isLoginInAdminList("anyone") {
		t.Fatalf("expected no admin match when ADMIN_LOGINS is empty")
	}
}

func TestIsLoginInAdminList_CaseInsensitiveLoginMatch(t *testing.T) {
	t.Setenv("ADMIN_LOGINS", "HeInZ")
	resetAdminLoginCacheForTest()

	if !isLoginInAdminList("heinz") {
		t.Fatalf("expected case-insensitive ADMIN_LOGINS match")
	}
}
