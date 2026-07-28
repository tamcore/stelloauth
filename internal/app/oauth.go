package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

type ProgressFunc func(step string)
type DebugFunc func(msg string)

func performOAuth(req OAuthRequest, requestID string, progress ProgressFunc, debug DebugFunc) (string, error) {
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
	authURL := fmt.Sprintf(
		"%s/am/oauth2/authorize?client_id=%s&response_type=code"+
			"&redirect_uri=%s&scope=openid%%20profile%%20email&locale=%s",
		brandConfig.OAuthURL,
		countryConfig.ClientID,
		url.QueryEscape(redirectURI),
		countryConfig.Locale,
	)

	log.Printf("[%s] Starting OAuth flow for %s/%s", requestID, req.Brand, req.Country)

	// Use chromedp to automate the login flow
	code, err := performChromedpOAuth(authURL, req.Email, req.Password, brandConfig.Scheme, requestID, progress, debug)
	if err != nil {
		return "", err
	}

	return code, nil
}

func performChromedpOAuth(
	authURL, email, password, scheme, requestID string,
	progress ProgressFunc, debug DebugFunc,
) (string, error) {
	// Serialize browser use (CloakBrowser free tier = 1 session).
	if err := sessionGate.Acquire(context.Background(), func() {
		if progress != nil {
			progress("Waiting for a free browser slot...")
		}
	}); err != nil {
		if err == ErrSessionBusy {
			return "", fmt.Errorf("service is busy, please try again in a few seconds")
		}
		return "", err
	}
	defer sessionGate.Release()

	// Report real elapsed time via a heartbeat goroutine (the sole progress
	// writer); the flow below only updates the phase label via setPhase. This
	// keeps progress moving during the long blocking waits (page load, login)
	// that would otherwise be silent.
	setPhase, stopHeartbeat := startProgressHeartbeat(progress)
	defer stopHeartbeat()

	// Create context with timeout. Some brands (e.g. Opel) are slow and a full
	// login + consent can take ~2 minutes, so allow generous headroom.
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Connect to the CloakBrowser stealth-Chromium CDP endpoint. CloakBrowser
	// owns the fingerprint, so we pass no Chrome flags of our own.
	// Use the requestID as a unique fingerprint so each request gets an isolated
	// CloakBrowser session (avoids state leaking/wedging between requests).
	cdpURL := os.Getenv("CLOAK_CDP_URL")
	wsURL, err := discoverCDPWebSocketURL(cdpURL, requestID, &http.Client{Timeout: 10 * time.Second})
	if err != nil {
		return "", fmt.Errorf("browser backend unavailable: %v", err)
	}

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(ctx, wsURL)
	defer allocCancel()

	// Create browser context
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	var oauthCode string
	var flowError string // captured Stellantis OPErrorPage.php error, if any
	redirectPrefix := scheme + "://"

	// Domains relevant to the OAuth flow (for debug output filtering)
	relevantDomains := []string{
		"stellantis.com", "gigya.com",
		"peugeot.com", "citroen.com", "opel.com", "vauxhall.com", "dsautomobiles.com",
	}
	isRelevantURL := func(u string) bool {
		for _, domain := range relevantDomains {
			if strings.Contains(u, domain) {
				return true
			}
		}
		return false
	}

	// Set up listener for network events to catch the redirect (which fails because browser can't load custom schemes)
	chromedp.ListenTarget(browserCtx, func(ev any) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			reqURL := e.Request.URL
			// Capture OAuth redirect
			if strings.HasPrefix(reqURL, redirectPrefix) {
				log.Printf("[%s] Redirect URL: %s", requestID, reqURL)
				parsed, err := url.Parse(reqURL)
				if err == nil {
					if code := parsed.Query().Get("code"); code != "" {
						oauthCode = code
						log.Printf("[%s] Captured OAuth code from redirect request", requestID)
					}
				}
			} else if strings.Contains(reqURL, "OPErrorPage.php") {
				// Stellantis redirects here when the flow fails (e.g. an expired
				// contextId when the login took too long).
				if parsed, perr := url.Parse(reqURL); perr == nil {
					flowError = friendlyOPError(parsed.Query().Get("code"), parsed.Query().Get("message"))
					log.Printf("[%s] Stellantis error page: %s", requestID, reqURL)
				}
			} else if debug != nil && isRelevantURL(reqURL) {
				// Only show relevant OAuth flow URLs in debug output
				debug(fmt.Sprintf("Fetching: %s", reqURL))
			}
		}
	})

	// Selectors for Gigya login form (used by Stellantis)
	const (
		emailSelector    = `#gigya-login-form input[name="username"]`
		passwordSelector = `#gigya-login-form input[name="password"]`
		submitSelector   = `#gigya-login-form input[type="submit"]`
	)

	// Possible authorization form selectors (different pages use different forms)
	// Order matters - more specific selectors first
	// ForgeRock AM uses name="decision" with value="allow" or name="allow"
	authorizeSelectors := []string{
		`#consentbutton`, // DCR consent page ("CONTINUE") — post-login authorize
		`input[name="decision"][value="allow"]`,
		`button[name="decision"][value="allow"]`,
		`#allow`,
		`input[name="allow"]`,
		`button[name="allow"]`,
		`input[type="submit"][value="Allow"]`,
		`input[type="submit"][value="Erlauben"]`,  // German
		`input[type="submit"][value="Autoriser"]`, // French
		`#cvs_from input[type="submit"]`,
	}

	// Run the OAuth flow
	setPhase("Loading login page")
	err = chromedp.Run(browserCtx,
		network.Enable(),
		chromedp.Navigate(authURL),
		chromedp.WaitReady("body"),
	)
	if err != nil {
		return "", fmt.Errorf("failed to navigate: %v", err)
	}

	// Wait for the Gigya login form to appear
	setPhase("Waiting for login form")
	err = chromedp.Run(browserCtx,
		chromedp.WaitVisible(emailSelector, chromedp.ByQuery),
	)
	if err != nil {
		// Log what we see on the page
		var pageHTML string
		_ = chromedp.Run(browserCtx, chromedp.OuterHTML("html", &pageHTML))
		log.Printf("Page HTML length: %d", len(pageHTML))
		return "", fmt.Errorf("login form not found (timeout): %v", err)
	}

	// Wait until the whole login form (incl. submit button) has rendered, then
	// type credentials with real key events so Gigya's validation fires.
	setPhase("Entering credentials")
	// Type credentials with real key events (Gigya's validation and bot scoring
	// need genuine input). SendKeys focuses the node itself, so no explicit Click
	// (which fails with "not focusable" while the Gigya screenset is still
	// wiring up). Settle briefly first for a cold browser.
	err = chromedp.Run(browserCtx,
		chromedp.WaitVisible(passwordSelector, chromedp.ByQuery),
		chromedp.WaitVisible(submitSelector, chromedp.ByQuery),
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.SendKeys(emailSelector, email, chromedp.ByQuery),
		chromedp.SendKeys(passwordSelector, password, chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),
	)
	if err != nil {
		return "", fmt.Errorf("failed to fill credentials: %v", err)
	}

	// Submit login form using Click
	setPhase("Signing in")
	err = chromedp.Run(browserCtx,
		chromedp.Click(submitSelector, chromedp.ByQuery),
	)
	if err != nil {
		return "", fmt.Errorf("failed to submit login: %v", err)
	}

	// Grace period for a direct redirect (heartbeat reports elapsed time).
	for range 5 {
		if oauthCode != "" || flowError != "" {
			break
		}
		_ = chromedp.Run(browserCtx, chromedp.Sleep(2*time.Second))
	}

	// Check if we captured the code already (direct redirect)
	if oauthCode != "" {
		setPhase("Authentication successful")
		return oauthCode, nil
	}

	// Check for login errors
	var errorText string
	_ = chromedp.Run(browserCtx,
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
	setPhase("Waiting for authorization")

	// Give the page time to render
	_ = chromedp.Run(browserCtx, chromedp.Sleep(2*time.Second))

	// Handle the post-login authorization/consent page (if present) and wait
	// for the resulting redirect (or an error page).
	clickAuthorizeAndWait(browserCtx, authorizeSelectors, requestID, setPhase,
		func() bool { return oauthCode != "" || flowError != "" })

	// If we captured the code, return it
	if oauthCode != "" {
		setPhase("Authentication successful")
		return oauthCode, nil
	}

	// Surface a Stellantis error page (e.g. expired contextId) with a clear message.
	if flowError != "" {
		if flowError == msgSessionExpired {
			return "", errSessionExpired
		}
		return "", fmt.Errorf("authentication failed: %s", flowError)
	}

	// Last resort: the redirect may already be the current URL.
	if code := codeFromLocation(browserCtx, redirectPrefix); code != "" {
		return code, nil
	}

	return "", fmt.Errorf("authentication failed - could not retrieve OAuth code")
}

// clickAuthorizeAndWait looks for a post-login authorization/consent control
// (trying each selector with a short timeout), clicks the first one found, and
// then waits briefly for the OAuth redirect to be captured. codeSet reports
// whether the redirect code has already arrived.
func clickAuthorizeAndWait(
	browserCtx context.Context, selectors []string, requestID string,
	setPhase func(string), codeSet func() bool,
) {
	var authorizeFound bool
	for _, selector := range selectors {
		checkCtx, checkCancel := context.WithTimeout(browserCtx, 3*time.Second)
		err := chromedp.Run(checkCtx, chromedp.WaitVisible(selector, chromedp.ByQuery))
		checkCancel()

		if err == nil {
			authorizeFound = true
			setPhase("Confirming authorization")
			log.Printf("[%s] Found authorize button with selector: %s", requestID, selector)
			_ = chromedp.Run(browserCtx, chromedp.Click(selector, chromedp.ByQuery))
			break
		}
	}

	if !authorizeFound {
		return
	}

	setPhase("Waiting for redirect")
	for range 5 {
		if codeSet() {
			break
		}
		_ = chromedp.Run(browserCtx, chromedp.Sleep(2*time.Second))
	}
}

// startProgressHeartbeat runs a goroutine that emits "<phase>... (Ns)" every
// second (real elapsed time) until stop() is called. It is the sole progress
// writer; callers change the reported phase via the returned setPhase. stop()
// waits for the goroutine to exit so it never writes concurrently with a later
// event. When progress is nil, setPhase is a no-op sink and stop() does nothing.
func startProgressHeartbeat(progress ProgressFunc) (setPhase func(string), stop func()) {
	var mu sync.Mutex
	phase := "Starting browser"
	setPhase = func(p string) {
		mu.Lock()
		phase = p
		mu.Unlock()
	}
	if progress == nil {
		return setPhase, func() {}
	}

	start := time.Now()
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				mu.Lock()
				p := phase
				mu.Unlock()
				progress(fmt.Sprintf("%s... (%ds)", p, int(time.Since(start).Seconds())))
			}
		}
	})
	return setPhase, func() { close(done); wg.Wait() }
}

// msgSessionExpired is the user-facing message for an expired Stellantis
// consent contextId (login took too long). errSessionExpired is the sentinel
// returned for that case so callers can distinguish it (e.g. to refund the
// rate limit — it is a transient, not-the-user's-fault failure).
const msgSessionExpired = "the login took too long and the session expired, please try again"

var errSessionExpired = errors.New(msgSessionExpired)

// friendlyOPError turns a Stellantis OPErrorPage.php code/message into a
// user-facing error string. Stellantis encodes spaces as '+', so it is decoded
// back to spaces; the expired-contextId case gets a clear retry hint.
func friendlyOPError(code, message string) string {
	msg := strings.ReplaceAll(message, "+", " ")
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "expired") || strings.Contains(lower, "took too long") {
		return msgSessionExpired
	}
	if msg != "" {
		return msg
	}
	if code != "" {
		return "login failed (" + code + ")"
	}
	return "login failed"
}

// codeFromLocation returns the OAuth code if the current page URL is the
// custom-scheme redirect carrying it, otherwise "".
func codeFromLocation(browserCtx context.Context, redirectPrefix string) string {
	var currentURL string
	_ = chromedp.Run(browserCtx, chromedp.Location(&currentURL))
	log.Printf("Current URL: %s", currentURL)
	if !strings.HasPrefix(currentURL, redirectPrefix) {
		return ""
	}
	parsed, err := url.Parse(currentURL)
	if err != nil {
		return ""
	}
	return parsed.Query().Get("code")
}
