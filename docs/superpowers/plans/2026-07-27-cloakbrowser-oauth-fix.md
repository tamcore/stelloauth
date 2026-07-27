# CloakBrowser Stealth Sidecar OAuth Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route stelloauth's chromedp OAuth flow through a CloakBrowser stealth-Chromium CDP sidecar so it clears Stellantis' invisible reCAPTCHA, and bound concurrency to the free tier's single session.

**Architecture:** stelloauth stops launching its own headless Chrome. Instead it connects chromedp to a CloakBrowser CDP endpoint (`CLOAK_CDP_URL`) via `NewRemoteAllocator`. A session gate (buffered channel) serializes browser use and emits a "waiting" progress message. In Kubernetes, CloakBrowser runs as a sidecar container; for local dev it is baked into `Dockerfile.dev`.

**Tech Stack:** Go 1.26.0, chromedp v0.14.2, Helm, Docker. Spike-proven end-to-end (Peugeot/DE → real OAuth code).

## Global Constraints

- Go module `github.com/tamcore/stelloauth`, `go 1.26.0`. No new module dependencies — use stdlib only (`net/http`, `encoding/json`, buffered `chan` for the semaphore).
- chromedp pinned at `v0.14.2` (has `NewRemoteAllocator`).
- **CloakBrowser-only.** `CLOAK_CDP_URL` is REQUIRED; `main()` must `log.Fatal` if it is empty. The old `NewExecAllocator` path and all Chrome flags are removed.
- New env vars and defaults: `CLOAK_CDP_URL` (required, e.g. `http://localhost:9222`), `CLOAK_MAX_SESSIONS` (default `1`), `CLOAK_QUEUE_TIMEOUT` (default `60s`).
- Immutable style; small focused files; errors handled explicitly with user-facing messages.
- Consent page control is `#consentbutton` (value "CONTINUE"); it must be tried before the existing ForgeRock authorize selectors.

---

### Task 1: CDP websocket discovery

**Files:**
- Create: `browser.go`
- Test: `browser_test.go`

**Interfaces:**
- Produces: `func discoverCDPWebSocketURL(cdpBaseURL string, client *http.Client) (string, error)` — GETs `{base}/json/version`, returns `webSocketDebuggerUrl`.

- [ ] **Step 1: Write the failing test**

Create `browser_test.go`:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverCDPWebSocketURL_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"Browser":"Chrome/146","webSocketDebuggerUrl":"ws://host:9222/devtools/browser/abc"}`))
	}))
	defer srv.Close()

	ws, err := discoverCDPWebSocketURL(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ws != "ws://host:9222/devtools/browser/abc" {
		t.Errorf("got %q", ws)
	}
}

func TestDiscoverCDPWebSocketURL_TrailingSlash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			t.Errorf("path not normalized: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"webSocketDebuggerUrl":"ws://x/y"}`))
	}))
	defer srv.Close()

	if _, err := discoverCDPWebSocketURL(srv.URL+"/", srv.Client()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiscoverCDPWebSocketURL_MissingField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Browser":"Chrome/146"}`))
	}))
	defer srv.Close()

	if _, err := discoverCDPWebSocketURL(srv.URL, srv.Client()); err == nil {
		t.Fatal("expected error for missing webSocketDebuggerUrl")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestDiscoverCDPWebSocketURL ./...`
Expected: FAIL — `undefined: discoverCDPWebSocketURL`.

- [ ] **Step 3: Write minimal implementation**

Create `browser.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// cdpVersion is the subset of GET /json/version we consume.
type cdpVersion struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// discoverCDPWebSocketURL queries {cdpBaseURL}/json/version and returns the
// browser-level websocket debugger URL for chromedp's remote allocator.
func discoverCDPWebSocketURL(cdpBaseURL string, client *http.Client) (string, error) {
	url := strings.TrimRight(cdpBaseURL, "/") + "/json/version"
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("querying CDP endpoint %s: %w", url, err)
	}
	defer resp.Body.Close()

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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestDiscoverCDPWebSocketURL ./...`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add browser.go browser_test.go
git commit -m "feat(browser): discover CloakBrowser CDP websocket URL

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

### Task 2: Session gate (concurrency guard)

**Files:**
- Create: `session.go`
- Test: `session_test.go`

**Interfaces:**
- Produces:
  - `var ErrSessionBusy error`
  - `type SessionGate struct{ ... }`
  - `func newSessionGate(maxSessions int, waitTimeout time.Duration) *SessionGate`
  - `func (g *SessionGate) Acquire(ctx context.Context, onWait func()) error` — returns `nil` on acquire, `ErrSessionBusy` on wait timeout, `ctx.Err()` on cancel. `onWait` (nil-safe) fires once if the caller must block.
  - `func (g *SessionGate) Release()`

- [ ] **Step 1: Write the failing test**

Create `session_test.go`:

```go
package main

import (
	"context"
	"testing"
	"time"
)

func TestSessionGate_AcquireImmediateNoWait(t *testing.T) {
	g := newSessionGate(1, time.Second)
	waited := false
	if err := g.Acquire(context.Background(), func() { waited = true }); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if waited {
		t.Error("onWait should not fire when a slot is free")
	}
	g.Release()
}

func TestSessionGate_SecondCallWaitsThenTimesOut(t *testing.T) {
	g := newSessionGate(1, 50*time.Millisecond)
	if err := g.Acquire(context.Background(), nil); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	waited := false
	err := g.Acquire(context.Background(), func() { waited = true })
	if err != ErrSessionBusy {
		t.Fatalf("expected ErrSessionBusy, got %v", err)
	}
	if !waited {
		t.Error("onWait should have fired while blocking")
	}
	g.Release()
}

func TestSessionGate_ReleaseLetsNextAcquire(t *testing.T) {
	g := newSessionGate(1, time.Second)
	_ = g.Acquire(context.Background(), nil)
	go func() {
		time.Sleep(20 * time.Millisecond)
		g.Release()
	}()
	if err := g.Acquire(context.Background(), nil); err != nil {
		t.Fatalf("expected acquire after release, got %v", err)
	}
	g.Release()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestSessionGate ./...`
Expected: FAIL — `undefined: newSessionGate`.

- [ ] **Step 3: Write minimal implementation**

Create `session.go`:

```go
package main

import (
	"context"
	"errors"
	"time"
)

// ErrSessionBusy is returned when no browser session frees up within the wait timeout.
var ErrSessionBusy = errors.New("all browser sessions are busy")

// SessionGate bounds concurrent browser sessions. CloakBrowser's free tier
// allows a single concurrent session, so the default capacity is 1.
type SessionGate struct {
	slots       chan struct{}
	waitTimeout time.Duration
}

func newSessionGate(maxSessions int, waitTimeout time.Duration) *SessionGate {
	if maxSessions < 1 {
		maxSessions = 1
	}
	return &SessionGate{
		slots:       make(chan struct{}, maxSessions),
		waitTimeout: waitTimeout,
	}
}

// Acquire reserves a session slot. If one is free it returns immediately.
// Otherwise onWait (if non-nil) is invoked once and the call blocks until a
// slot frees up, the wait timeout elapses (ErrSessionBusy), or ctx is done.
func (g *SessionGate) Acquire(ctx context.Context, onWait func()) error {
	select {
	case g.slots <- struct{}{}:
		return nil
	default:
	}

	if onWait != nil {
		onWait()
	}

	timer := time.NewTimer(g.waitTimeout)
	defer timer.Stop()

	select {
	case g.slots <- struct{}{}:
		return nil
	case <-timer.C:
		return ErrSessionBusy
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release returns a previously acquired slot. Safe to call at most once per Acquire.
func (g *SessionGate) Release() {
	select {
	case <-g.slots:
	default:
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestSessionGate ./...`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add session.go session_test.go
git commit -m "feat(session): add concurrency gate for single-session browser

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

### Task 3: Startup config (require CLOAK_CDP_URL, init gate)

**Files:**
- Create: `config.go`
- Modify: `main.go` (the `main()` function, lines ~156-169)
- Test: `config_test.go`

**Interfaces:**
- Consumes: `newSessionGate` (Task 2).
- Produces:
  - `func getIntEnv(key string, def int) int`
  - `func getDurationEnv(key string, def time.Duration) time.Duration`
  - package-level `var sessionGate *SessionGate`

- [ ] **Step 1: Write the failing test**

Create `config_test.go`:

```go
package main

import (
	"testing"
	"time"
)

func TestGetIntEnv(t *testing.T) {
	t.Setenv("CLOAK_MAX_SESSIONS", "3")
	if got := getIntEnv("CLOAK_MAX_SESSIONS", 1); got != 3 {
		t.Errorf("got %d, want 3", got)
	}
	if got := getIntEnv("CLOAK_MISSING_INT", 7); got != 7 {
		t.Errorf("default not used: got %d", got)
	}
	t.Setenv("CLOAK_MAX_SESSIONS", "notanint")
	if got := getIntEnv("CLOAK_MAX_SESSIONS", 1); got != 1 {
		t.Errorf("invalid value should fall back to default, got %d", got)
	}
}

func TestGetDurationEnv(t *testing.T) {
	t.Setenv("CLOAK_QUEUE_TIMEOUT", "90s")
	if got := getDurationEnv("CLOAK_QUEUE_TIMEOUT", 60*time.Second); got != 90*time.Second {
		t.Errorf("got %v, want 90s", got)
	}
	if got := getDurationEnv("CLOAK_MISSING_DUR", 60*time.Second); got != 60*time.Second {
		t.Errorf("default not used: got %v", got)
	}
	t.Setenv("CLOAK_QUEUE_TIMEOUT", "garbage")
	if got := getDurationEnv("CLOAK_QUEUE_TIMEOUT", 60*time.Second); got != 60*time.Second {
		t.Errorf("invalid value should fall back to default, got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run 'TestGetIntEnv|TestGetDurationEnv' ./...`
Expected: FAIL — `undefined: getIntEnv`.

- [ ] **Step 3: Write minimal implementation**

Create `config.go`:

```go
package main

import (
	"log"
	"os"
	"strconv"
	"time"
)

// sessionGate serializes browser use across requests (see SessionGate).
var sessionGate *SessionGate

func getIntEnv(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("invalid %s=%q, using default %d", key, v, def)
		return def
	}
	return n
}

func getDurationEnv(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("invalid %s=%q, using default %s", key, v, def)
		return def
	}
	return d
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run 'TestGetIntEnv|TestGetDurationEnv' ./...`
Expected: PASS.

- [ ] **Step 5: Wire startup into `main()`**

In `main.go`, replace the current start of `main()`:

```go
func main() {
	port := getEnv("PORT", defaultPort)
	address := getEnv("HTTP_ADDRESS", defaultAddress)

	initRateLimiter()
```

with:

```go
func main() {
	port := getEnv("PORT", defaultPort)
	address := getEnv("HTTP_ADDRESS", defaultAddress)

	if os.Getenv("CLOAK_CDP_URL") == "" {
		log.Fatal("CLOAK_CDP_URL is required: set it to the CloakBrowser CDP endpoint (e.g. http://localhost:9222)")
	}
	sessionGate = newSessionGate(
		getIntEnv("CLOAK_MAX_SESSIONS", 1),
		getDurationEnv("CLOAK_QUEUE_TIMEOUT", 60*time.Second),
	)

	initRateLimiter()
```

- [ ] **Step 6: Run the full test suite + build**

Run: `go build ./... && go test ./...`
Expected: builds; all tests pass. (`TestMain` calls `initRateLimiter`; `sessionGate` is only used by `performChromedpOAuth`, so unit tests remain green without `CLOAK_CDP_URL`.)

- [ ] **Step 7: Commit**

```bash
git add config.go config_test.go main.go
git commit -m "feat(config): require CLOAK_CDP_URL, init session gate at startup

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

### Task 4: Route the OAuth flow through CloakBrowser + flow fixes

**Files:**
- Modify: `main.go` (`performChromedpOAuth`, lines ~337-467)

**Interfaces:**
- Consumes: `discoverCDPWebSocketURL` (Task 1), `sessionGate` (Task 3), `chromedp.NewRemoteAllocator`.

This flow needs a live CloakBrowser + real credentials, so it is verified by build/vet, the existing unit suite, and a manual e2e checkpoint rather than a new unit test.

- [ ] **Step 1: Acquire a session slot at the top of `performChromedpOAuth`**

`performChromedpOAuth` currently begins:

```go
func performChromedpOAuth(authURL, email, password, scheme, requestID string, progress ProgressFunc, debug DebugFunc) (string, error) {
	if progress != nil {
		progress("Starting browser...")
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
```

Insert the gate acquisition immediately after the function opens (before the browser context), so queuing time does not count against the 120s browser deadline:

```go
func performChromedpOAuth(authURL, email, password, scheme, requestID string, progress ProgressFunc, debug DebugFunc) (string, error) {
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

	if progress != nil {
		progress("Starting browser...")
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
```

- [ ] **Step 2: Replace the local allocator with the remote CloakBrowser allocator**

Replace this block:

```go
	// Create chromedp options for headless browser
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("incognito", true),
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
```

with:

```go
	// Connect to the CloakBrowser stealth-Chromium CDP endpoint. CloakBrowser
	// owns the fingerprint, so we pass no Chrome flags of our own.
	cdpURL := os.Getenv("CLOAK_CDP_URL")
	wsURL, err := discoverCDPWebSocketURL(cdpURL, &http.Client{Timeout: 10 * time.Second})
	if err != nil {
		return "", fmt.Errorf("browser backend unavailable: %v", err)
	}

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(ctx, wsURL)
	defer allocCancel()

	// Create browser context
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()
```

Note: this introduces a first `err` via `:=`. The existing code later declares `err := chromedp.Run(...)` at the navigate step — change that later `:=` to `=` (Step 5).

- [ ] **Step 3: Add the consent-page selector**

In the `authorizeSelectors` slice, add `#consentbutton` as the first entry:

```go
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
```

- [ ] **Step 4: Wait for the submit button before filling; type with real key events**

Replace the credential-fill + submit block:

```go
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
	)
	if err != nil {
		return "", fmt.Errorf("failed to submit login: %v", err)
	}
```

with (wait for password AND submit button to render before interacting; use `SendKeys` so Gigya sees real input events):

```go
	// Wait until the whole login form (incl. submit button) has rendered, then
	// type credentials with real key events so Gigya's validation fires.
	if progress != nil {
		progress("Entering credentials...")
	}
	err = chromedp.Run(browserCtx,
		chromedp.WaitVisible(passwordSelector, chromedp.ByQuery),
		chromedp.WaitVisible(submitSelector, chromedp.ByQuery),
		chromedp.Click(emailSelector, chromedp.ByQuery),
		chromedp.SendKeys(emailSelector, email, chromedp.ByQuery),
		chromedp.Click(passwordSelector, chromedp.ByQuery),
		chromedp.SendKeys(passwordSelector, password, chromedp.ByQuery),
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
	)
	if err != nil {
		return "", fmt.Errorf("failed to submit login: %v", err)
	}
```

- [ ] **Step 5: Fix the `err` redeclaration at the navigate step**

Because Step 2 now declares `err` earlier in the function, change the navigate block from `:=` to `=`:

```go
	err = chromedp.Run(browserCtx,
		network.Enable(),
		chromedp.Navigate(authURL),
		chromedp.WaitReady("body"),
	)
	if err != nil {
		return "", fmt.Errorf("failed to navigate: %v", err)
	}
```

- [ ] **Step 6: Verify imports and build**

Ensure `main.go` imports include `net/http` (already present) and `os` (already present). Remove no imports unless `go build` reports one unused.

Run: `go vet ./... && go build ./... && go test ./...`
Expected: builds cleanly; all existing unit tests pass.

- [ ] **Step 7: Commit**

```bash
git add main.go
git commit -m "feat(oauth): drive login via CloakBrowser CDP, fix submit timing + consent

Connect chromedp to CLOAK_CDP_URL via NewRemoteAllocator (no local Chrome
flags), gate concurrency, wait for the submit button before typing with real
key events, and click the DCR consent button (#consentbutton).

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

- [ ] **Step 8: Manual e2e checkpoint (requires a running CloakBrowser + real creds)**

Start CloakBrowser locally and run the e2e suite against it:

```bash
docker run -d --name cloak -p 127.0.0.1:9222:9222 cloakhq/cloakbrowser cloakserve
export CLOAK_CDP_URL=http://localhost:9222
set -a && source .envrc && set +a
./e2e/e2e_test.sh PEUGEOT
docker rm -f cloak
```

Expected: `PASS: Got OAuth code`. If it stalls, re-check `#consentbutton` and the submit-button wait. This is the definitive verification for Task 4.

---

### Task 5: Helm sidecar + concurrency env

**Files:**
- Modify: `charts/stelloauth/templates/deployment.yaml`
- Modify: `charts/stelloauth/values.yaml`

**Interfaces:**
- Consumes: env vars from Task 3/4 (`CLOAK_CDP_URL`, `CLOAK_MAX_SESSIONS`, `CLOAK_QUEUE_TIMEOUT`).

- [ ] **Step 1: Add values**

Append to `charts/stelloauth/values.yaml`:

```yaml
# CloakBrowser stealth-Chromium sidecar (drives the OAuth login, clears reCAPTCHA).
cloakbrowser:
  enabled: true
  image:
    repository: cloakhq/cloakbrowser
    tag: "latest"
    pullPolicy: IfNotPresent
  # CloakBrowser's cloakserve binds this port inside the pod. localhost-only;
  # not exposed on the Service.
  port: 9222
  resources: {}

# Concurrency guard. CloakBrowser's free tier allows one session; raise
# maxSessions only with a CloakBrowser Pro license.
cloak:
  maxSessions: 1
  queueTimeout: "60s"
```

- [ ] **Step 2: Add env to the app container**

In `charts/stelloauth/templates/deployment.yaml`, inside the app container's `env:` block, immediately after the `{{- end }}` that closes the rateLimit block and before the `{{- range $key, $value := .Values.env }}` line, insert:

```yaml
            {{- if .Values.cloakbrowser.enabled }}
            - name: CLOAK_CDP_URL
              value: "http://localhost:{{ .Values.cloakbrowser.port }}"
            {{- end }}
            - name: CLOAK_MAX_SESSIONS
              value: {{ .Values.cloak.maxSessions | quote }}
            - name: CLOAK_QUEUE_TIMEOUT
              value: {{ .Values.cloak.queueTimeout | quote }}
```

- [ ] **Step 3: Add the sidecar container**

In `charts/stelloauth/templates/deployment.yaml`, immediately after the app container's `resources:` block (the `{{- toYaml .Values.resources | nindent 12 }}` line) and before the `{{- with .Values.nodeSelector }}` line, add the sidecar as a second list item under `containers:`:

```yaml
        {{- if .Values.cloakbrowser.enabled }}
        - name: cloakbrowser
          image: "{{ .Values.cloakbrowser.image.repository }}:{{ .Values.cloakbrowser.image.tag }}"
          imagePullPolicy: {{ .Values.cloakbrowser.image.pullPolicy }}
          args: ["cloakserve"]
          ports:
            - name: cdp
              containerPort: {{ .Values.cloakbrowser.port }}
              protocol: TCP
          resources:
            {{- toYaml .Values.cloakbrowser.resources | nindent 12 }}
        {{- end }}
```

Indentation: the `- name: cloakbrowser` item must align with `- name: {{ .Chart.Name }}` (8 spaces) so both are items of the same `containers:` list.

- [ ] **Step 4: Verify the chart renders**

Run:
```bash
helm template charts/stelloauth | grep -nE 'name: cloakbrowser|CLOAK_CDP_URL|CLOAK_MAX_SESSIONS|containerPort: 9222'
```
Expected: shows the sidecar container, the three env vars, and the CDP containerPort. If `helm` is not installed, run it via Docker: `docker run --rm -v "$PWD/charts:/charts" alpine/helm template /charts/stelloauth | grep -n cloakbrowser`.

- [ ] **Step 5: Commit**

```bash
git add charts/stelloauth/values.yaml charts/stelloauth/templates/deployment.yaml
git commit -m "feat(chart): add CloakBrowser sidecar and concurrency env

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

### Task 6: Local dev — bake CloakBrowser into Dockerfile.dev + compose

**Files:**
- Modify: `Dockerfile.dev`
- Create: `docker-entrypoint.dev.sh`
- Modify: `docker-compose.yaml`
- Modify: `README.md` (Docker Compose / dev section)

**Interfaces:**
- Consumes: `CLOAK_CDP_URL` (Task 3/4).

- [ ] **Step 1: Rewrite `Dockerfile.dev` to a single dev image with CloakBrowser baked in**

Replace the whole file with:

```dockerfile
# Build stage
FROM golang:latest AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o stelloauth .

# Runtime: the CloakBrowser image bundles stealth Chrome + cloakserve.
FROM cloakhq/cloakbrowser:latest

COPY --from=builder /app/stelloauth /usr/local/bin/stelloauth
COPY docker-entrypoint.dev.sh /usr/local/bin/docker-entrypoint.dev.sh

ENV CLOAK_CDP_URL=http://localhost:9222
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.dev.sh"]
```

- [ ] **Step 2: Create the dev entrypoint that runs cloakserve + the app**

Create `docker-entrypoint.dev.sh` (make it executable in Step 3):

```sh
#!/bin/sh
set -e

# Start the CloakBrowser CDP server in the background.
cloakserve &

# Wait for the CDP endpoint to come up.
echo "waiting for CloakBrowser CDP on :9222..."
i=0
until wget -qO- http://localhost:9222/json/version >/dev/null 2>&1; do
	i=$((i + 1))
	if [ "$i" -gt 60 ]; then
		echo "CloakBrowser did not start within 60s" >&2
		exit 1
	fi
	sleep 1
done
echo "CloakBrowser is up; starting stelloauth."

exec /usr/local/bin/stelloauth
```

- [ ] **Step 3: Build and run the dev image to confirm both processes start**

```bash
chmod +x docker-entrypoint.dev.sh
docker build -f Dockerfile.dev -t stelloauth:dev .
docker run --rm -p 8080:8080 --name stelloauth-dev stelloauth:dev &
sleep 25
curl -sf http://localhost:8080/ >/dev/null && echo "app OK"
docker rm -f stelloauth-dev
```
Expected: logs show "CloakBrowser is up; starting stelloauth" and `app OK`.
If `cloakserve` is not on `PATH` in the base image, inspect it with `docker run --rm --entrypoint sh cloakhq/cloakbrowser -c 'command -v cloakserve || ls /usr/local/bin'` and adjust the entrypoint's invocation accordingly.

- [ ] **Step 4: Update `docker-compose.yaml` for the two-container prod-image path**

Replace `docker-compose.yaml` with:

```yaml
services:
  cloakbrowser:
    image: cloakhq/cloakbrowser:latest
    command: ["cloakserve"]
    restart: unless-stopped

  stelloauth:
    image: ghcr.io/tamcore/stelloauth:latest
    # build:
    #   context: .
    #   dockerfile: Dockerfile.dev
    depends_on:
      - cloakbrowser
    ports:
      - "8080:8080"
    environment:
      CLOAK_CDP_URL: "http://cloakbrowser:9222"
      # Optional: concurrency guard (defaults: 1 session, 60s queue wait)
      # CLOAK_MAX_SESSIONS: "1"
      # CLOAK_QUEUE_TIMEOUT: "60s"
      # Optional: rate limiting
      # RATE_LIMIT_COUNT: "10"
      # RATE_LIMIT_DURATION: "1h"
    restart: unless-stopped
```

- [ ] **Step 5: Update the README dev/compose note**

In `README.md`, under the Docker Compose section, add a sentence:

```markdown
The compose stack runs a CloakBrowser sidecar (`cloakbrowser`) that stelloauth connects to via `CLOAK_CDP_URL`. CloakBrowser drives the login in a stealth Chromium to pass Stellantis' bot protection. The free CloakBrowser tier allows one concurrent login at a time; additional requests queue briefly.
```

- [ ] **Step 6: Commit**

```bash
git add Dockerfile.dev docker-entrypoint.dev.sh docker-compose.yaml README.md
git commit -m "feat(dev): bake CloakBrowser into Dockerfile.dev, add compose sidecar

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

### Task 7 (optional): Slim the production app image

The production `Dockerfile` still bases on `chromedp/headless-shell` (bundled Chrome) which the app no longer launches. This is optional cleanup — do it only if you also confirm goreleaser still builds.

**Files:**
- Modify: `Dockerfile`

- [ ] **Step 1: Switch to a minimal base**

Replace `Dockerfile` with:

```dockerfile
# Runtime stage - the app no longer launches Chrome (CloakBrowser sidecar does).
FROM alpine:latest

RUN apk add --no-cache ca-certificates

# Copy binary from goreleaser build context
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/stelloauth /usr/local/bin/stelloauth

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/stelloauth"]
```

- [ ] **Step 2: Verify the image builds with a static binary**

```bash
CGO_ENABLED=0 GOOS=linux go build -o /tmp/stelloauth-lin .
mkdir -p /tmp/ctx/linux/amd64 && cp /tmp/stelloauth-lin /tmp/ctx/linux/amd64/stelloauth
docker build -f Dockerfile --build-arg TARGETPLATFORM=linux/amd64 -t stelloauth:slim /tmp/ctx 2>&1 | tail -3 || echo "adjust context to match goreleaser layout"
```
Expected: builds. (The goreleaser `dockers_v2` config supplies `${TARGETPLATFORM}/stelloauth`; this manual check just confirms the Dockerfile is valid.)

- [ ] **Step 3: Commit**

```bash
git add Dockerfile
git commit -m "chore(docker): slim prod image, drop bundled Chrome

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Self-Review

**Spec coverage:**
- §1 Browser allocation (CloakBrowser-only, fail-fast, RemoteAllocator, ws discovery) → Task 1 + Task 3 Step 5 + Task 4 Steps 2/6.
- §2 Flow robustness (submit-wait, SendKeys, `#consentbutton`) → Task 4 Steps 3/4.
- §3 Concurrency guard (semaphore, queue, timeout, waiting progress) → Task 2 + Task 4 Step 1.
- §4 Deployment (helm sidecar, CLOAK_CDP_URL, 9222 not on Service) → Task 5.
- §5 Config fix → already committed (`ea84f67`), not repeated here.
- Error handling (CDP-unreachable fail fast, queue-timeout message) → Task 4 Steps 1/2.
- Testing (unit for gate/discovery/env, e2e checkpoint) → Tasks 1/2/3 units, Task 4 Step 8, Task 5 Step 4.
- Local dev (bake-in) → Task 6.

**Placeholder scan:** No TBD/TODO; all code blocks concrete.

**Type consistency:** `discoverCDPWebSocketURL(string, *http.Client) (string, error)`, `newSessionGate(int, time.Duration)`, `SessionGate.Acquire(context.Context, func()) error`, `SessionGate.Release()`, `getIntEnv`, `getDurationEnv`, `sessionGate` — consistent across Tasks 1-4.
