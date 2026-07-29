package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/google/uuid"
)

// workerRequest is the worker-v2-compatible OAuth request: the caller supplies a
// fully-built authorize URL instead of brand/country. This mirrors the API of
// github.com/andreadegiovine/homeassistant-stellantis-vehicles-worker-v2 so the
// Home Assistant integration's custom "Login service URL" can point at stelloauth.
type workerRequest struct {
	URL      string `json:"url"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// workerCode is the 200 response body: {"code": "<oauth-code>"}.
type workerCode struct {
	Code string `json:"code"`
}

// workerError is the failure response body: {"message": "...", "code": <status>},
// mirroring worker-v2 where the numeric code doubles as the HTTP status.
type workerError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// handleWorker is the worker-v2-compatible endpoint. Unlike /oauth it receives a
// pre-built authorize URL and returns the code at the top level.
func handleWorker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendWorkerError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientIP := getClientIP(r)
	if !rateLimiter.isAllowed(clientIP) {
		log.Printf("Rate limit exceeded for %s (worker)", clientIP)
		sendWorkerError(w, "Rate limit exceeded. Try again later.", http.StatusTooManyRequests)
		return
	}

	var req workerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendWorkerError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.URL == "" || req.Email == "" || req.Password == "" {
		sendWorkerError(w, "Missing required params", http.StatusBadRequest)
		return
	}

	scheme, err := redirectScheme(req.URL)
	if err != nil {
		sendWorkerError(w, err.Error(), http.StatusBadRequest)
		return
	}

	requestID := uuid.New().String()
	log.Printf("[%s] worker OAuth request from %s for user %s", requestID, clientIP, req.Email)

	code, err := performChromedpOAuth(req.URL, req.Email, req.Password, scheme, requestID, nil, nil)
	if err != nil {
		refundIfExpired(clientIP, requestID, err)
		log.Printf("[%s] worker OAuth failed: %s", requestID, err.Error())
		sendWorkerError(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("[%s] worker OAuth successful", requestID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(workerCode{Code: code})
}

// redirectScheme extracts the custom redirect scheme (e.g. "mymap") from the
// authorize URL's redirect_uri query parameter; the code-capture listener keys on
// "<scheme>://".
func redirectScheme(authURL string) (string, error) {
	u, err := url.Parse(authURL)
	if err != nil {
		return "", fmt.Errorf("invalid url: %v", err)
	}
	redirect := u.Query().Get("redirect_uri")
	if redirect == "" {
		return "", errors.New("invalid url: missing redirect_uri")
	}
	ru, err := url.Parse(redirect)
	if err != nil || ru.Scheme == "" {
		return "", errors.New("invalid url: unparseable redirect_uri")
	}
	return ru.Scheme, nil
}

func sendWorkerError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(workerError{Message: message, Code: statusCode})
}
