package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"watchdog/watchdog"
)

type authContextKey string

const authUserContextKey authContextKey = "auth_user"

type panbagnatRole struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type panbagnatUser struct {
	ID            string          `json:"id"`
	FtID          int             `json:"ft_id"`
	FtLogin       string          `json:"ft_login"`
	FtIsStaff     bool            `json:"ft_is_staff"`
	IsStaff       bool            `json:"is_staff"`
	IsBlacklisted bool            `json:"is_blacklisted"`
	Roles         []panbagnatRole `json:"roles"`
}

var (
	panbagnatURLOnce sync.Once
	panbagnatURL     *url.URL
	panbagnatURLErr  error

	adminRolesOnce sync.Once
	adminRolesSet  map[string]struct{}
)

var authHTTPClient = &http.Client{Timeout: 5 * time.Second}

func requireAdminAuth(next http.Handler) http.Handler {
	return requirePanbagnatAuth(next, true, true)
}

func requireUserAuth(next http.Handler) http.Handler {
	return requirePanbagnatAuth(next, false, false)
}

func requirePanbagnatAuth(next http.Handler, requireAdmin bool, allowTrustedLocal bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowTrustedLocal && isTrustedLocalCommandRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		mode := strings.ToLower(strings.TrimSpace(os.Getenv("AUTH_MODE")))
		if mode != "panbagnat" {
			watchdog.Log("[AUTH] Request rejected: AUTH_MODE must be set to panbagnat")
			writeJSONError(w, http.StatusServiceUnavailable, "auth_unavailable", "This route requires AUTH_MODE=panbagnat.")
			return
		}

		user, resp, err := fetchPanbagnatUser(r)
		if err != nil {
			watchdog.Log("[AUTH] Panbagnat auth request failed: " + err.Error())
			writeJSONError(w, http.StatusBadGateway, "auth_unreachable", "Panbagnat authentication is unavailable.")
			return
		}
		if resp != nil {
			watchdog.Log("[AUTH] Panbagnat rejected remote command request with status " + resp.Status)
			forwardAuthResponse(w, resp)
			return
		}
		if user == nil {
			watchdog.Log("[AUTH] Panbagnat returned no user for request")
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
			return
		}
		if user.IsBlacklisted {
			watchdog.Log("[AUTH] Blacklisted user " + user.FtLogin + " attempted a remote command")
			writeJSONError(w, http.StatusForbidden, "blacklisted", "Your account is currently blacklisted.")
			return
		}
		if requireAdmin && !isPanbagnatAdmin(*user) {
			watchdog.Log("[AUTH] User " + user.FtLogin + " is authenticated but not allowed to use this admin route")
			writeJSONError(w, http.StatusForbidden, "admin_required", "You are not allowed to use this endpoint.")
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authUserContextKey, user)))
	})
}

func getAuthenticatedUser(r *http.Request) *panbagnatUser {
	if r == nil {
		return nil
	}
	user, _ := r.Context().Value(authUserContextKey).(*panbagnatUser)
	return user
}

func isTrustedLocalCommandRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Forwarded") != "" {
		return false
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil || !ip.IsLoopback() {
		return false
	}

	reqHost := r.Host
	if h, _, err := net.SplitHostPort(reqHost); err == nil {
		reqHost = h
	}
	reqHost = strings.ToLower(strings.TrimSpace(reqHost))
	switch reqHost {
	case "", "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func fetchPanbagnatUser(r *http.Request) (*panbagnatUser, *http.Response, error) {
	base, err := getPanbagnatAuthURL()
	if err != nil {
		return nil, nil, err
	}

	urlStr, err := buildAuthURL(base, "/api/v1/users/me")
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, nil, err
	}
	copyAuthHeaders(r, req)

	client := authHTTPClient
	if shouldSkipPanbagnatTLSVerify() {
		client = cloneHTTPClientSkippingTLSVerify(client)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp, nil
	}
	defer resp.Body.Close()

	var user panbagnatUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, nil, err
	}

	return &user, nil, nil
}

func getPanbagnatAuthURL() (*url.URL, error) {
	panbagnatURLOnce.Do(func() {
		raw := strings.TrimSpace(os.Getenv("AUTH_PANBAGNAT_URL"))
		if raw == "" {
			raw = strings.TrimSpace(os.Getenv("AUTH_SERVICE_URL"))
		}
		if raw == "" {
			panbagnatURLErr = errors.New("panbagnat auth URL not configured")
			return
		}
		if !strings.Contains(raw, "://") {
			raw = "http://" + raw
		}
		panbagnatURL, panbagnatURLErr = url.Parse(raw)
	})
	if panbagnatURLErr != nil {
		return nil, panbagnatURLErr
	}
	if panbagnatURL == nil {
		return nil, errors.New("panbagnat auth URL not configured")
	}
	return panbagnatURL, nil
}

func buildAuthURL(base *url.URL, path string) (string, error) {
	if base == nil {
		return "", errors.New("auth base URL not configured")
	}
	target := *base
	basePath := strings.TrimRight(target.Path, "/")
	target.Path = basePath + path
	return target.String(), nil
}

func shouldSkipPanbagnatTLSVerify() bool {
	value := strings.TrimSpace(os.Getenv("AUTH_PANBAGNAT_SKIP_VERIFY"))
	return strings.EqualFold(value, "1") || strings.EqualFold(value, "true")
}

func cloneHTTPClientSkippingTLSVerify(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	if tr, ok := client.Transport.(*http.Transport); ok {
		cloned := tr.Clone()
		if cloned.TLSClientConfig == nil {
			cloned.TLSClientConfig = &tls.Config{}
		}
		cloned.TLSClientConfig.InsecureSkipVerify = true
		return &http.Client{Transport: cloned, Timeout: client.Timeout}
	}
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: client.Timeout,
	}
}

func copyAuthHeaders(src *http.Request, dst *http.Request) {
	if v := src.Header.Get("Cookie"); v != "" {
		dst.Header.Set("Cookie", v)
	}
	if v := src.Header.Get("Authorization"); v != "" {
		dst.Header.Set("Authorization", v)
	}
	if v := src.Header.Get("X-Session-Id"); v != "" {
		dst.Header.Set("X-Session-Id", v)
	}
	if v := src.Header.Get("User-Agent"); v != "" {
		dst.Header.Set("User-Agent", v)
	}
}

func forwardAuthResponse(w http.ResponseWriter, resp *http.Response) {
	if resp == nil {
		return
	}
	defer resp.Body.Close()
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	if code != "" {
		w.Header().Set("X-Error-Code", code)
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   http.StatusText(status),
		"code":    code,
		"message": message,
	})
}

func isPanbagnatAdmin(user panbagnatUser) bool {
	adminRolesOnce.Do(loadAdminRoles)
	if len(adminRolesSet) == 0 {
		return user.IsStaff || user.FtIsStaff
	}
	for _, role := range user.Roles {
		if _, ok := adminRolesSet[normalizeRole(role.ID)]; ok {
			return true
		}
		if _, ok := adminRolesSet[normalizeRole(role.Name)]; ok {
			return true
		}
	}
	return user.IsStaff || user.FtIsStaff
}

func loadAdminRoles() {
	adminRolesSet = make(map[string]struct{})
	raw := strings.TrimSpace(os.Getenv("AUTH_ADMIN_ROLES"))
	if raw == "" {
		return
	}
	for _, part := range strings.Split(raw, ",") {
		role := normalizeRole(part)
		if role != "" {
			adminRolesSet[role] = struct{}{}
		}
	}
}

func normalizeRole(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
