package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
)

//go:embed web/*
var webFS embed.FS

const (
	configsURL     = "https://raw.githubusercontent.com/andreadegiovine/homeassistant-stellantis-vehicles/develop/custom_components/stellantis_vehicles/configs.json"
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

	content, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

func handleConfigs(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get(configsURL)
	if err != nil {
		http.Error(w, "Failed to fetch configs", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
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

	code, err := performOAuth(req)
	if err != nil {
		sendError(w, err.Error(), http.StatusBadRequest)
		return
	}

	sendSuccess(w, code)
}

func performOAuth(req OAuthRequest) (string, error) {
	// Fetch configs
	resp, err := http.Get(configsURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch configs: %v", err)
	}
	defer resp.Body.Close()

	var configs map[string]BrandConfig
	if err := json.NewDecoder(resp.Body).Decode(&configs); err != nil {
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

	// Create HTTP client with cookie jar
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Check if this is the final redirect with the code
			if strings.HasPrefix(req.URL.String(), brandConfig.Scheme+"://") {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	// Build authorization URL
	redirectURI := fmt.Sprintf("%s://oauth2redirect/%s", brandConfig.Scheme, strings.ToLower(req.Country))
	authURL := fmt.Sprintf("%s/am/oauth2/authorize?client_id=%s&response_type=code&redirect_uri=%s&scope=openid%%20profile%%20email&locale=%s",
		brandConfig.OAuthURL,
		countryConfig.ClientID,
		url.QueryEscape(redirectURI),
		countryConfig.Locale,
	)

	// Step 1: Get the authorization page to obtain the login form
	authResp, err := client.Get(authURL)
	if err != nil {
		return "", fmt.Errorf("failed to access authorization page: %v", err)
	}
	defer authResp.Body.Close()

	authBody, err := io.ReadAll(authResp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read authorization page: %v", err)
	}

	// Extract the form action URL and any hidden fields
	formAction := extractFormAction(string(authBody), brandConfig.OAuthURL)
	if formAction == "" {
		return "", fmt.Errorf("could not find login form")
	}

	// Step 2: Submit the login form
	loginData := url.Values{}
	loginData.Set("username", req.Email)
	loginData.Set("password", req.Password)
	loginData.Set("rememberMe", "false")

	// Extract hidden fields from the form
	hiddenFields := extractHiddenFields(string(authBody))
	for k, v := range hiddenFields {
		loginData.Set(k, v)
	}

	loginReq, err := http.NewRequest(http.MethodPost, formAction, strings.NewReader(loginData.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create login request: %v", err)
	}
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginReq.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 10) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.120 Mobile Safari/537.36")

	loginResp, err := client.Do(loginReq)
	if err != nil {
		return "", fmt.Errorf("failed to submit login: %v", err)
	}
	defer loginResp.Body.Close()

	// Check for the redirect with the code
	location := loginResp.Header.Get("Location")
	if location != "" && strings.HasPrefix(location, brandConfig.Scheme+"://") {
		return extractCode(location)
	}

	// Read the response body to check for errors or further redirects
	loginBody, _ := io.ReadAll(loginResp.Body)

	// Check if we need to follow more redirects
	if loginResp.StatusCode >= 300 && loginResp.StatusCode < 400 {
		return followRedirects(client, location, brandConfig.Scheme)
	}

	// Check for error messages in the response
	if strings.Contains(string(loginBody), "error") || strings.Contains(string(loginBody), "invalid") {
		return "", fmt.Errorf("authentication failed - please check your credentials")
	}

	// Try to find a redirect URL in the page
	redirectURL := extractRedirectURL(string(loginBody))
	if redirectURL != "" {
		return followRedirects(client, redirectURL, brandConfig.Scheme)
	}

	return "", fmt.Errorf("authentication failed - could not complete OAuth flow")
}

func extractFormAction(html string, baseURL string) string {
	// Look for form action in the HTML
	re := regexp.MustCompile(`<form[^>]*action=["']([^"']+)["']`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		action := matches[1]
		if strings.HasPrefix(action, "/") {
			return baseURL + action
		}
		return action
	}
	return ""
}

func extractHiddenFields(html string) map[string]string {
	fields := make(map[string]string)
	re := regexp.MustCompile(`<input[^>]*type=["']hidden["'][^>]*name=["']([^"']+)["'][^>]*value=["']([^"']*)["']`)
	matches := re.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) > 2 {
			fields[match[1]] = match[2]
		}
	}

	// Also try the reverse order (value before name)
	re2 := regexp.MustCompile(`<input[^>]*value=["']([^"']*)["'][^>]*type=["']hidden["'][^>]*name=["']([^"']+)["']`)
	matches2 := re2.FindAllStringSubmatch(html, -1)
	for _, match := range matches2 {
		if len(match) > 2 {
			fields[match[2]] = match[1]
		}
	}

	return fields
}

func extractCode(urlStr string) (string, error) {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse redirect URL: %v", err)
	}

	code := parsed.Query().Get("code")
	if code == "" {
		return "", fmt.Errorf("no code found in redirect URL")
	}

	return code, nil
}

func extractRedirectURL(html string) string {
	// Look for meta refresh or JavaScript redirects
	re := regexp.MustCompile(`(?:window\.location|location\.href)\s*=\s*["']([^"']+)["']`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return matches[1]
	}

	re2 := regexp.MustCompile(`<meta[^>]*http-equiv=["']refresh["'][^>]*content=["'][^"']*url=([^"']+)["']`)
	matches2 := re2.FindStringSubmatch(html)
	if len(matches2) > 1 {
		return matches2[1]
	}

	return ""
}

func followRedirects(client *http.Client, startURL string, scheme string) (string, error) {
	currentURL := startURL
	for i := 0; i < 10; i++ {
		if strings.HasPrefix(currentURL, scheme+"://") {
			return extractCode(currentURL)
		}

		resp, err := client.Get(currentURL)
		if err != nil {
			return "", fmt.Errorf("failed to follow redirect: %v", err)
		}
		defer resp.Body.Close()

		location := resp.Header.Get("Location")
		if location == "" {
			body, _ := io.ReadAll(resp.Body)
			location = extractRedirectURL(string(body))
		}

		if location == "" {
			break
		}

		if strings.HasPrefix(location, scheme+"://") {
			return extractCode(location)
		}

		currentURL = location
	}

	return "", fmt.Errorf("failed to complete redirect chain")
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
