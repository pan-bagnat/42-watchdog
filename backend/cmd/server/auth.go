package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"watchdog/watchdog"
)

type authContextKey string

const authUserContextKey authContextKey = "auth_user"

type authRole struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type authUser struct {
	ID            string     `json:"id"`
	FtID          int        `json:"ft_id"`
	FtLogin       string     `json:"ft_login"`
	FtIsStaff     bool       `json:"ft_is_staff"`
	IsStaff       bool       `json:"is_staff"`
	IsBlacklisted bool       `json:"is_blacklisted"`
	PhotoURL      string     `json:"ft_photo,omitempty"`
	Roles         []authRole `json:"roles,omitempty"`
}

type authMeResponse struct {
	*authUser
	BadgeDelaySeconds *int64 `json:"badge_delay_seconds,omitempty"`
}

type userProvider interface {
	FetchUser(r *http.Request, requireAdmin bool) (*authUser, *http.Response, error)
}

type localAuthProvider struct {
	baseURL    *url.URL
	httpClient *http.Client
}

type panbagnatProvider struct {
	baseURL    *url.URL
	httpClient *http.Client
}

var (
	authHTTPClient = &http.Client{Timeout: 5 * time.Second}

	userProviderOnce sync.Once
	userProviderErr  error
	userProviderInst userProvider

	authServiceURLOnce sync.Once
	authServiceURL     *url.URL
	authServiceURLErr  error

	panbagnatURLOnce sync.Once
	panbagnatURL     *url.URL
	panbagnatURLErr  error

	adminRolesOnce sync.Once
	adminRolesSet  map[string]struct{}
)

func requireAdminAuth(next http.Handler) http.Handler {
	return requireAuth(next, true, true)
}

func requireUserAuth(next http.Handler) http.Handler {
	return requireAuth(next, false, false)
}

func requireAuth(next http.Handler, requireAdmin bool, allowTrustedLocal bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowTrustedLocal && isTrustedLocalCommandRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		user, resp, err := fetchAuthUser(r, requireAdmin)
		if err != nil {
			watchdog.Log("[AUTH] Auth request failed: " + err.Error())
			writeJSONError(w, http.StatusBadGateway, "auth_unreachable", "Authentication service is unavailable.")
			return
		}
		if resp != nil {
			statusLabel := resp.Status
			if statusLabel == "" {
				statusLabel = http.StatusText(resp.StatusCode)
			}
			watchdog.Log("[AUTH] Auth service rejected request with status " + statusLabel)
			forwardAuthResponse(w, resp)
			return
		}
		if user == nil {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
			return
		}
		if user.IsBlacklisted {
			watchdog.Log("[AUTH] Blacklisted user " + user.FtLogin + " attempted to access a protected route")
			writeJSONError(w, http.StatusForbidden, "blacklisted", "Your account is currently blacklisted.")
			return
		}
		if !user.IsStaff && !user.FtIsStaff {
			if err := watchdog.EnsureKnownUser(user.FtLogin, user.FtIsStaff); err != nil {
				watchdog.Log("[AUTH] Failed to ensure watchdog user " + user.FtLogin + ": " + err.Error())
			}
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authUserContextKey, user)))
	})
}

func getAuthenticatedUser(r *http.Request) *authUser {
	if r == nil {
		return nil
	}
	user, _ := r.Context().Value(authUserContextKey).(*authUser)
	return user
}

func fetchAuthUser(r *http.Request, requireAdmin bool) (*authUser, *http.Response, error) {
	provider, err := getUserProvider()
	if err != nil {
		return nil, nil, err
	}
	return provider.FetchUser(r, requireAdmin)
}

func getUserProvider() (userProvider, error) {
	userProviderOnce.Do(func() {
		if isPanbagnatAuthMode() {
			base, err := getPanbagnatAuthURL()
			if err != nil {
				userProviderErr = err
				return
			}
			userProviderInst = &panbagnatProvider{
				baseURL:    base,
				httpClient: authHTTPClient,
			}
			return
		}

		base, err := getAuthServiceURL()
		if err != nil {
			userProviderErr = err
			return
		}
		userProviderInst = &localAuthProvider{
			baseURL:    base,
			httpClient: authHTTPClient,
		}
	})

	if userProviderErr != nil {
		return nil, userProviderErr
	}
	if userProviderInst == nil {
		return nil, errors.New("auth provider not configured")
	}
	return userProviderInst, nil
}

func authMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("AUTH_MODE")))
	if mode == "" {
		return "local"
	}
	return mode
}

func isPanbagnatAuthMode() bool {
	return authMode() == "panbagnat"
}

func (p *localAuthProvider) FetchUser(r *http.Request, requireAdmin bool) (*authUser, *http.Response, error) {
	if p == nil || p.baseURL == nil {
		return nil, nil, errors.New("auth service URL not configured")
	}

	path := "/internal/auth/user"
	if requireAdmin {
		path = "/internal/auth/admin"
	}

	urlStr, err := buildAuthURL(p.baseURL, path)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, nil, err
	}
	copyAuthHeaders(r, req)

	client := p.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp, nil
	}
	defer resp.Body.Close()

	var user authUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, nil, err
	}
	return &user, nil, nil
}

func (p *panbagnatProvider) FetchUser(r *http.Request, requireAdmin bool) (*authUser, *http.Response, error) {
	if p == nil || p.baseURL == nil {
		return nil, nil, errors.New("panbagnat auth URL not configured")
	}

	urlStr, err := buildAuthURL(p.baseURL, "/api/v1/users/me")
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, nil, err
	}
	copyAuthHeaders(r, req)

	client := p.httpClient
	if client == nil {
		client = http.DefaultClient
	}
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

	var user authUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, nil, err
	}
	if requireAdmin && !isPanbagnatAdmin(user) {
		return nil, newAuthErrorResponse(http.StatusForbidden, "admin_required", "You are not allowed to use this endpoint."), nil
	}

	return &user, nil, nil
}

func getAuthServiceURL() (*url.URL, error) {
	authServiceURLOnce.Do(func() {
		raw := strings.TrimSpace(os.Getenv("AUTH_SERVICE_URL"))
		if raw == "" {
			raw = "http://auth:8080"
		}
		if !strings.Contains(raw, "://") {
			raw = "http://" + raw
		}
		authServiceURL, authServiceURLErr = url.Parse(raw)
	})
	if authServiceURLErr != nil {
		return nil, authServiceURLErr
	}
	if authServiceURL == nil {
		return nil, errors.New("auth service URL not configured")
	}
	return authServiceURL, nil
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
	if resp.StatusCode != 0 {
		w.WriteHeader(resp.StatusCode)
	}
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

func isPanbagnatAdmin(user authUser) bool {
	adminRolesOnce.Do(loadAdminRoles)
	for _, role := range user.Roles {
		if _, ok := adminRolesSet[normalizeRole(role.ID)]; ok {
			return true
		}
		if _, ok := adminRolesSet[normalizeRole(role.Name)]; ok {
			return true
		}
	}
	return false
}

func isUserAllowedLoginOverride(user *authUser) bool {
	if user == nil {
		return false
	}
	if isPanbagnatAuthMode() {
		return isPanbagnatAdmin(*user)
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

func authMeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := getAuthenticatedUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}

	delay := watchdog.SnapshotBadgeDelay()
	var badgeDelaySeconds *int64
	if delay.Known {
		value := int64(delay.Delay / time.Second)
		badgeDelaySeconds = &value
	}

	writeJSON(w, http.StatusOK, authMeResponse{
		authUser:          user,
		BadgeDelaySeconds: badgeDelaySeconds,
	})
}

func authLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if isPanbagnatAuthMode() {
		redirectToExternalAuth(w, r, "AUTH_PANBAGNAT_LOGIN_URL", "/auth/42/login")
		return
	}
	proxyBrowserAuthRoute(w, r, false)
}

func authCallbackHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if isPanbagnatAuthMode() {
		http.NotFound(w, r)
		return
	}
	proxyBrowserAuthRoute(w, r, false)
}

func authLogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	proxyBrowserAuthRoute(w, r, isPanbagnatAuthMode())
}

func redirectToExternalAuth(w http.ResponseWriter, r *http.Request, specificEnv, fallbackPath string) {
	target := strings.TrimSpace(os.Getenv(specificEnv))
	if target == "" {
		base, err := getPanbagnatAuthURL()
		if err != nil {
			watchdog.Log("[AUTH] Could not resolve panbagnat login URL: " + err.Error())
			writeJSONError(w, http.StatusBadGateway, "auth_unreachable", "Authentication service is unavailable.")
			return
		}
		target, err = buildAuthURL(base, fallbackPath)
		if err != nil {
			watchdog.Log("[AUTH] Could not build panbagnat login URL: " + err.Error())
			writeJSONError(w, http.StatusBadGateway, "auth_unreachable", "Authentication service is unavailable.")
			return
		}
	}

	parsed, err := url.Parse(target)
	if err != nil {
		watchdog.Log("[AUTH] Invalid auth redirect URL: " + err.Error())
		writeJSONError(w, http.StatusBadGateway, "auth_unreachable", "Authentication service is unavailable.")
		return
	}
	if parsed.RawQuery == "" {
		parsed.RawQuery = r.URL.RawQuery
	} else if r.URL.RawQuery != "" {
		parsed.RawQuery = parsed.RawQuery + "&" + r.URL.RawQuery
	}
	http.Redirect(w, r, parsed.String(), http.StatusFound)
}

func proxyBrowserAuthRoute(w http.ResponseWriter, r *http.Request, rewriteHost bool) {
	target, err := getBrowserAuthBaseURL()
	if err != nil {
		watchdog.Log("[AUTH] Browser auth proxy setup failed: " + err.Error())
		writeJSONError(w, http.StatusBadGateway, "auth_unreachable", "Authentication service is unavailable.")
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = joinURLPath(target.Path, r.URL.Path)
		req.URL.RawPath = ""
		req.URL.RawQuery = r.URL.RawQuery
		if rewriteHost {
			req.Host = target.Host
		} else {
			req.Host = r.Host
		}
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
		watchdog.Log("[AUTH] Browser auth proxy error: " + proxyErr.Error())
		writeJSONError(rw, http.StatusBadGateway, "auth_unreachable", "Authentication service is unavailable.")
	}
	proxy.ServeHTTP(w, r)
}

func getBrowserAuthBaseURL() (*url.URL, error) {
	if isPanbagnatAuthMode() {
		return getPanbagnatAuthURL()
	}
	return getAuthServiceURL()
}

func joinURLPath(basePath, requestPath string) string {
	base := strings.TrimRight(basePath, "/")
	req := requestPath
	if req == "" {
		req = "/"
	}
	if !strings.HasPrefix(req, "/") {
		req = "/" + req
	}
	if base == "" {
		return req
	}
	return base + req
}

func newAuthErrorResponse(status int, code, message string) *http.Response {
	body, _ := json.Marshal(map[string]string{
		"error":   http.StatusText(status),
		"code":    code,
		"message": message,
	})
	return &http.Response{
		Status:     http.StatusText(status),
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(bytes.NewReader(body)),
	}
}
