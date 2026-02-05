package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

//go:embed web/index.html
var indexHTML []byte

//go:embed configs.json
var configsJSON []byte

const (
	defaultPort    = "8080"
	defaultAddress = "0.0.0.0"
)

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

func main() {
	port := getEnv("PORT", defaultPort)
	address := getEnv("HTTP_ADDRESS", defaultAddress)

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/configs", handleConfigs)
	http.HandleFunc("/oauth", handleOAuth)

	addr := fmt.Sprintf("%s:%s", address, port)
	log.Printf("Starting server on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

func handleConfigs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(configsJSON)
}

func handleOAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, "Method not allowed", http.StatusMethodNotAllowed)
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

	// Check if client accepts SSE
	if r.Header.Get("Accept") == "text/event-stream" {
		handleOAuthSSE(w, req)
		return
	}

	code, err := performOAuth(req, nil)
	if err != nil {
		sendError(w, err.Error(), http.StatusBadRequest)
		return
	}

	sendSuccess(w, code)
}

func handleOAuthSSE(w http.ResponseWriter, req OAuthRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		sendError(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	progress := func(step string) {
		fmt.Fprintf(w, "data: {\"type\":\"progress\",\"message\":\"%s\"}\n\n", step)
		flusher.Flush()
	}

	code, err := performOAuth(req, progress)
	if err != nil {
		fmt.Fprintf(w, "data: {\"type\":\"error\",\"message\":\"%s\"}\n\n", err.Error())
		flusher.Flush()
		return
	}

	fmt.Fprintf(w, "data: {\"type\":\"success\",\"code\":\"%s\"}\n\n", code)
	flusher.Flush()
}

type ProgressFunc func(step string)

func performOAuth(req OAuthRequest, progress ProgressFunc) (string, error) {
	if progress != nil {
		progress("Preparing authentication...")
	}

	// Parse embedded configs
	var configs map[string]BrandConfig
	if err := json.Unmarshal(configsJSON, &configs); err != nil {
		return "", fmt.Errorf("failed to parse configs: %v", err)
	}

	brandConfig, ok := configs[req.Brand]
	if !ok {
		return "", fmt.Errorf("unknown brand: %s", req.Brand)
	}

	countryConfig, ok := brandConfig.Configs[req.Country]
	if !ok {
		return "", fmt.Errorf("unknown country for brand %s: %s", req.Brand, req.Country)
	}

	// Build authorization URL
	redirectURI := fmt.Sprintf("%s://oauth2redirect/%s", brandConfig.Scheme, strings.ToLower(req.Country))
	authURL := fmt.Sprintf("%s/am/oauth2/authorize?client_id=%s&response_type=code&redirect_uri=%s&scope=openid%%20profile%%20email&locale=%s",
		brandConfig.OAuthURL,
		countryConfig.ClientID,
		url.QueryEscape(redirectURI),
		countryConfig.Locale,
	)

	log.Printf("Starting OAuth flow for %s/%s", req.Brand, req.Country)

	// Use chromedp to automate the login flow
	code, err := performChromedpOAuth(authURL, req.Email, req.Password, brandConfig.Scheme, progress)
	if err != nil {
		return "", err
	}

	return code, nil
}

func performChromedpOAuth(authURL, email, password, scheme string, progress ProgressFunc) (string, error) {
	if progress != nil {
		progress("Starting browser...")
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Create chromedp options for headless browser
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	// Create browser context
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	var oauthCode string
	var authError string
	redirectPrefix := scheme + "://"

	// Set up listener for network events to catch the redirect (which fails because browser can't load custom schemes)
	chromedp.ListenTarget(browserCtx, func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			reqURL := e.Request.URL
			if strings.HasPrefix(reqURL, redirectPrefix) {
				parsed, err := url.Parse(reqURL)
				if err == nil {
					if code := parsed.Query().Get("code"); code != "" {
						oauthCode = code
						log.Printf("Captured OAuth code from redirect request")
					}
				}
			}
		case *network.EventLoadingFailed:
			// Also catch failed loads for the custom scheme
			if oauthCode == "" {
				log.Printf("Network loading failed: %s", e.ErrorText)
			}
		}
	})

	// Selectors for Gigya login form (used by Stellantis)
	const (
		emailSelector    = `#gigya-login-form input[name="username"]`
		passwordSelector = `#gigya-login-form input[name="password"]`
		submitSelector   = `#gigya-login-form input[type="submit"]`
		authorizeSelector = `#cvs_from input[type="submit"]`
	)

	// Run the OAuth flow
	if progress != nil {
		progress("Loading login page...")
	}
	err := chromedp.Run(browserCtx,
		network.Enable(),
		chromedp.Navigate(authURL),
		chromedp.WaitReady("body"),
	)
	if err != nil {
		return "", fmt.Errorf("failed to navigate: %v", err)
	}

	// Wait for the Gigya login form to appear
	if progress != nil {
		progress("Waiting for login form...")
	}
	err = chromedp.Run(browserCtx,
		chromedp.WaitVisible(emailSelector, chromedp.ByQuery),
	)
	if err != nil {
		// Log what we see on the page
		var pageHTML string
		chromedp.Run(browserCtx, chromedp.OuterHTML("html", &pageHTML))
		log.Printf("Page HTML length: %d", len(pageHTML))
		if strings.Contains(pageHTML, "error") || strings.Contains(pageHTML, "Error") {
			authError = "login page error"
		}
		return "", fmt.Errorf("login form not found (timeout): %v", err)
	}

	// Fill in credentials using SetValue (more reliable for SPAs)
	if progress != nil {
		progress("Entering credentials...")
	}
	err = chromedp.Run(browserCtx,
		chromedp.WaitVisible(passwordSelector, chromedp.ByQuery),
		chromedp.SetValue(emailSelector, email, chromedp.ByQuery),
		chromedp.SetValue(passwordSelector, password, chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
	)
	if err != nil {
		return "", fmt.Errorf("failed to fill credentials: %v", err)
	}

	// Submit login form using Click
	if progress != nil {
		progress("Submitting login...")
	}
	err = chromedp.Run(browserCtx,
		chromedp.Click(submitSelector, chromedp.ByQuery),
		chromedp.Sleep(5*time.Second),
	)
	if err != nil {
		return "", fmt.Errorf("failed to submit login: %v", err)
	}

	// Check if we captured the code already (direct redirect)
	if oauthCode != "" {
		if progress != nil {
			progress("Authentication successful!")
		}
		return oauthCode, nil
	}

	// Check for login errors
	var errorText string
	chromedp.Run(browserCtx,
		chromedp.Evaluate(`
			(function() {
				var error = document.querySelector('.gigya-error-msg, .error-message, [class*="error"]');
				if (error && error.textContent.trim()) {
					return error.textContent.trim();
				}
				return '';
			})()
		`, &errorText),
	)
	if errorText != "" {
		return "", fmt.Errorf("authentication failed: %s", errorText)
	}

	// Wait for authorization confirmation page (if present)
	if progress != nil {
		progress("Waiting for authorization...")
	}
	err = chromedp.Run(browserCtx,
		chromedp.WaitVisible(authorizeSelector, chromedp.ByQuery),
	)
	if err == nil {
		// Found authorize form, click it
		if progress != nil {
			progress("Confirming authorization...")
		}
		chromedp.Run(browserCtx,
			chromedp.Click(authorizeSelector, chromedp.ByQuery),
			chromedp.Sleep(3*time.Second),
		)
	}

	// Wait a bit more for redirect
	chromedp.Run(browserCtx, chromedp.Sleep(5*time.Second))

	// If we captured the code, return it
	if oauthCode != "" {
		return oauthCode, nil
	}

	// Check current URL
	var currentURL string
	chromedp.Run(browserCtx, chromedp.Location(&currentURL))
	log.Printf("Current URL: %s", currentURL)

	if strings.HasPrefix(currentURL, redirectPrefix) {
		parsed, err := url.Parse(currentURL)
		if err == nil {
			if code := parsed.Query().Get("code"); code != "" {
				return code, nil
			}
		}
	}

	if authError != "" {
		return "", fmt.Errorf("authentication failed: %s", authError)
	}

	return "", fmt.Errorf("authentication failed - could not retrieve OAuth code")
}

func sendError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(OAuthResponse{
		Status:  "error",
		Message: message,
	})
}

func sendSuccess(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OAuthResponse{
		Status: "success",
		Data: &OAuthData{
			Code: code,
		},
	})
}
