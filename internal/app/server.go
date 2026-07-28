package app

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/google/uuid"
)

//go:embed web/index.html
var indexHTML []byte

//go:embed configs.json
var configsJSON []byte

var countryDB *CountryDB

type OAuthRequest struct {
	Brand    string `json:"brand"`
	Country  string `json:"country"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type OAuthResponse struct {
	Status  string     `json:"status"`
	Message string     `json:"message,omitempty"`
	Data    *OAuthData `json:"data,omitempty"`
}

type OAuthData struct {
	Code string `json:"code"`
}

type BrandConfig struct {
	OAuthURL string                   `json:"oauth_url"`
	Realm    string                   `json:"realm"`
	Scheme   string                   `json:"scheme"`
	Configs  map[string]CountryConfig `json:"configs"`
}

type CountryConfig struct {
	Locale       string `json:"locale"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

func handleConfigs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(configsJSON)
}

func handleGeo(w http.ResponseWriter, r *http.Request) {
	country := ""
	if ip, ok := parseClientIP(getClientIP(r)); ok {
		country = countryDB.Country(ip)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"country": country})
}

// parseClientIP turns getClientIP's output (a bare IP from a proxy header, or an
// "ip:port" RemoteAddr) into a netip.Addr.
func parseClientIP(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	if ap, err := netip.ParseAddr(s); err == nil {
		return ap, true
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		if ap, err := netip.ParseAddr(host); err == nil {
			return ap, true
		}
	}
	return netip.Addr{}, false
}

func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for reverse proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to RemoteAddr
	return r.RemoteAddr
}

// refundIfExpired gives back the rate-limit charge when the OAuth attempt
// failed with a transient "session expired" error (bounded by the limiter).
func refundIfExpired(clientIP, requestID string, err error) {
	if errors.Is(err, errSessionExpired) && rateLimiter.refund(clientIP) {
		log.Printf("[%s] session expired — refunded rate-limit slot for %s", requestID, clientIP)
	}
}

func handleOAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get client IP early for rate limiting
	clientIP := getClientIP(r)

	// Check rate limit
	if !rateLimiter.isAllowed(clientIP) {
		remaining := rateLimiter.remaining(clientIP)
		log.Printf("Rate limit exceeded for %s (remaining: %d)", clientIP, remaining)
		sendError(w, "Rate limit exceeded. Try again later.", http.StatusTooManyRequests)
		return
	}

	var req OAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Brand == "" || req.Country == "" || req.Email == "" || req.Password == "" {
		sendError(w, "All fields are required", http.StatusBadRequest)
		return
	}

	// Generate request ID
	requestID := uuid.New().String()

	log.Printf("[%s] OAuth request from %s for user %s (%s/%s)", requestID, clientIP, req.Email, req.Brand, req.Country)

	// Check if client accepts SSE
	if r.Header.Get("Accept") == "text/event-stream" {
		handleOAuthSSE(w, req, requestID, clientIP)
		return
	}

	code, err := performOAuth(req, requestID, nil, nil)
	if err != nil {
		refundIfExpired(clientIP, requestID, err)
		log.Printf("[%s] OAuth failed: %s", requestID, err.Error())
		sendError(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("[%s] OAuth successful", requestID)
	sendSuccess(w, code)
}

func handleOAuthSSE(w http.ResponseWriter, req OAuthRequest, requestID, clientIP string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		sendError(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	progress := func(step string) {
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"progress\",\"message\":\"%s\"}\n\n", step)
		flusher.Flush()
	}

	debug := func(msg string) {
		// Escape quotes for JSON
		escaped := strings.ReplaceAll(msg, "\"", "\\\"")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"debug\",\"message\":\"%s\"}\n\n", escaped)
		flusher.Flush()
	}

	code, err := performOAuth(req, requestID, progress, debug)
	if err != nil {
		refundIfExpired(clientIP, requestID, err)
		log.Printf("[%s] OAuth failed: %s", requestID, err.Error())
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"error\",\"message\":\"%s\"}\n\n", err.Error())
		flusher.Flush()
		return
	}

	log.Printf("[%s] OAuth successful", requestID)
	_, _ = fmt.Fprintf(w, "data: {\"type\":\"success\",\"code\":\"%s\"}\n\n", code)
	flusher.Flush()
}

func sendError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(OAuthResponse{
		Status:  "error",
		Message: message,
	})
}

func sendSuccess(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(OAuthResponse{
		Status: "success",
		Data: &OAuthData{
			Code: code,
		},
	})
}
