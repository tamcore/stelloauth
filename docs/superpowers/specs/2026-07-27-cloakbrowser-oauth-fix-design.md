# Design: Fix broken OAuth via CloakBrowser stealth sidecar

**Date:** 2026-07-27
**Status:** Approved design, pending spec review

## Problem

stelloauth's automated OAuth login stopped working for all brands. Every request runs the
full 120s chromedp timeout and returns `authentication failed - could not retrieve OAuth
code` (HTTP 400).

## Root cause (evidenced)

Stellantis added an **invisible reCAPTCHA** to the Gigya login page
(`id-dcr.<brand>.com/gigya/OPLoginPage.php`). Confirmed from a live headless run of
Peugeot/DE:

- reCAPTCHA anchor `.../api2/anchor?...k=6Leh…WwpW&size=invisible`
- Gigya captcha wrapper flagged `data-error-flags="captchaNeeded"`
- injected error string: *"Captcha-Problem, bitte schließen Sie Ihren Browser…"*

Our headless Chrome (`--headless`, `--no-sandbox`, `--incognito`, spoofed desktop UA) scores
as a bot, so the invisible captcha never clears → submit goes nowhere.

Two secondary flow bugs (real, independent of captcha):
- submit is clicked before the Gigya screenset finishes painting (pre-submit screenshot was
  blank).
- `SetValue` sets `.value` but does not dispatch the `input`/`change` events Gigya listens for.

## Solution (validated with a spike)

Replace the self-launched headless Chrome with **CloakBrowser** — a stealth Chromium (C++
source-level anti-detection patches) run as a **CDP sidecar**. chromedp connects to it via
`NewRemoteAllocator` instead of `NewExecAllocator`.

A throwaway spike drove the exact Peugeot/DE flow through CloakBrowser CDP and completed
end-to-end: cleared the reCAPTCHA, logged in, clicked consent, captured a 36-char OAuth code.

## Components & changes

### 1. Browser allocation (`main.go`, `performChromedpOAuth`)
- **CloakBrowser-only.** Required env `CLOAK_CDP_URL` (e.g. `http://localhost:9222`).
  - **Fail fast at startup** if `CLOAK_CDP_URL` is unset/empty (`log.Fatal`) — no
    silent-broken mode.
  - Per request: `GET {CLOAK_CDP_URL}/json/version` → `webSocketDebuggerUrl`; then
    `chromedp.NewRemoteAllocator(ctx, wsURL)`.
- The local `NewExecAllocator` path and all its Chrome flags
  (headless/no-sandbox/incognito/UA) are **removed**; CloakBrowser owns the fingerprint.
- Local dev must run a CloakBrowser container (documented in README / docker-compose).

### 2. Flow robustness (`main.go`)
- Before clicking submit, `WaitVisible` the submit button (not just the username field).
- Fill credentials with `SendKeys` (real key events) instead of `SetValue`.
- Add `#consentbutton` (DCR consent page "CONTINUE") to `authorizeSelectors`, ordered before
  the existing ForgeRock selectors. Keep existing selectors for other brands/pages.

### 3. Concurrency guard (`main.go`)
CloakBrowser free tier = **1 concurrent session**; demo serves <10 users.
- A weighted semaphore of size `CLOAK_MAX_SESSIONS` (default `1`) wraps the browser session.
- Requests that can't acquire immediately **queue** up to `CLOAK_QUEUE_TIMEOUT` (default e.g.
  60s). While waiting, the SSE endpoint emits progress: *"Waiting for a free browser slot…"*.
- On timeout, return a friendly error ("Service busy, please try again in a few seconds").
- Non-SSE POST path: same wait, plain error on timeout.

### 4. Deployment (Helm chart)
- Add a CloakBrowser **sidecar** container (`cloakhq/cloakbrowser cloakserve`, port 9222) to
  the stelloauth Deployment.
- Set `CLOAK_CDP_URL=http://localhost:9222` on the app container.
- Do **not** expose 9222 on the Service (localhost-only between containers).
- Sidecar resource requests/limits; values toggles (`cloakbrowser.enabled`, image tag,
  optional `CLOAKBROWSER_LICENSE_KEY` secret for Pro).

### 5. Config fix (`configs.json`)
- MyCitroen/PT `client_secret` (~L547) has a log timestamp
  `2025-01-26T22:15:13.330548262Z` spliced into the value. Restore the intended secret.

## Error handling
- Distinguish: captcha/login failure, consent-not-found, queue timeout, CDP-unreachable
  (sidecar down) — each with a distinct user-facing message.
- If `CLOAK_CDP_URL` set but `/json/version` unreachable, fail fast with a clear
  "browser backend unavailable" error rather than the 120s timeout.

## Testing
- Unit: semaphore/queue behaviour (acquire, wait, timeout), env parsing, ws discovery.
- e2e: `e2e_test.sh` unchanged in shape; run against an instance wired to a CloakBrowser
  sidecar. Add a concurrency case (two parallel requests → one served, one queued/served).
- Manual: full brand matrix via the sidecar.

## Out of scope
- User-driven (human-solves-captcha) redesign — not needed while CloakBrowser clears it.
- Multi-region/Pro autoscaling of sessions.

## Risks
- Stealth is an arms race; a future Stellantis/Google change could re-break it.
- Free binary pinned to v146 (Chrome 146); may drift from live Chrome over time.
- Proprietary sidecar binary — acceptable as an isolated container that never sees more than
  the login page it drives.
