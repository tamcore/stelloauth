package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
)

// cdpVersion is the subset of GET /json/version we consume.
type cdpVersion struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// discoverCDPWebSocketURL queries {cdpBaseURL}/json/version and returns the
// browser-level websocket debugger URL for chromedp's remote allocator.
//
// A non-empty fingerprint is passed to CloakBrowser's multiplexer as a query
// parameter, which yields an isolated browser session for that fingerprint.
// Using a unique fingerprint per request avoids state leaking between requests
// (the free tier keeps a single shared browser otherwise, which wedges).
func discoverCDPWebSocketURL(cdpBaseURL, fingerprint string, client *http.Client) (string, error) {
	url := strings.TrimRight(cdpBaseURL, "/") + "/json/version"
	if fingerprint != "" {
		url += "?fingerprint=" + neturl.QueryEscape(fingerprint)
	}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("querying CDP endpoint %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("CDP endpoint %s returned status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading CDP response: %w", err)
	}

	var v cdpVersion
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("parsing CDP response: %w", err)
	}
	if v.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("CDP endpoint %s did not return a webSocketDebuggerUrl", url)
	}
	return v.WebSocketDebuggerURL, nil
}
