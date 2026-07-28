# stelloauth — agent guide

Web tool that obtains a Stellantis OAuth authorization code for the
[Home Assistant Stellantis Vehicles integration](https://github.com/andreadegiovine/homeassistant-stellantis-vehicles).
Single Go binary with an embedded web UI. It drives the vendor login flow in a
browser and captures the `code` from the custom-scheme redirect.

## Architecture

- **App** (`internal/app`): HTTP server (`/`, `/configs`, `/oauth`). `/oauth` accepts
  JSON `{brand, country, email, password}` and returns the OAuth `code`. Sends
  live progress over Server-Sent Events when the client sends
  `Accept: text/event-stream`.
- **Browser automation**: `chromedp` connects over the Chrome DevTools Protocol
  to a **CloakBrowser** stealth-Chromium instance (a separate process/container).
  Stellantis' login is behind an invisible reCAPTCHA that plain headless Chrome
  cannot pass; CloakBrowser can. The app does **not** launch or bundle Chrome
  itself.
- **Login flow** (`performChromedpOAuth`): navigate the authorize URL → Gigya
  login form → submit → post-login consent page → capture the redirect `code`.

## Key files

| File | Responsibility |
|------|----------------|
| `cmd/stelloauth/main.go` | Process entrypoint |
| `internal/app/app.go` | Application startup |
| `internal/app/server.go` | HTTP handlers and embedded assets |
| `internal/app/oauth.go` | OAuth/chromedp flow and progress heartbeat |
| `internal/app/browser.go` | Discover the CloakBrowser CDP websocket URL |
| `internal/app/session.go` | Concurrency gate (bounded browser sessions) |
| `internal/app/configs.json` | Embedded brand/country OAuth config (mirrors the upstream HA integration's `configs.json`) |
| `e2e/e2e_test.sh` | End-to-end test harness |
| `charts/stelloauth/` | Helm chart (app + CloakBrowser sidecar) |

## Environment variables

| Var | Default | Notes |
|-----|---------|-------|
| `CLOAK_CDP_URL` | *required* | CloakBrowser CDP endpoint (e.g. `http://localhost:9222`). App exits at startup if unset. |
| `CLOAK_MAX_SESSIONS` | `1` | Max concurrent browser sessions (free CloakBrowser tier allows 1). |
| `CLOAK_QUEUE_TIMEOUT` | `60s` | How long a request waits for a free session. |
| `PORT` / `HTTP_ADDRESS` | `8080` / `0.0.0.0` | Server bind. |
| `RATE_LIMIT_COUNT` / `RATE_LIMIT_DURATION` | off | Per-IP rate limit; set both to enable. |
| `GEOIP_COUNTRY_DB` | unset | Path or URL to a GeoLite2-Country `.mmdb`/`.mmdb.gz`; enables IP-based country pre-selection. Unset disables it. |

## Common commands

```bash
go build ./cmd/stelloauth
go vet ./...
go test ./...
gofmt -l .          # must be empty
golangci-lint run   # CI gate
```

Run locally (needs a CloakBrowser CDP endpoint):

```bash
docker compose up -d          # app + CloakBrowser sidecar
# or run CloakBrowser yourself, then:
export CLOAK_CDP_URL=http://localhost:9222
go run ./cmd/stelloauth
```

E2E (drives a real login; requires a running CloakBrowser and real
credentials via env, e.g. `PEUGEOT_DE_USERNAME` / `PEUGEOT_DE_PASSWORD`):

```bash
export CLOAK_CDP_URL=http://localhost:9222
./e2e/e2e_test.sh [BRAND]     # BRAND: OPEL|PEUGEOT|CITROEN|MYDS|VAUXHALL
```

## Deployment

Helm chart in `charts/stelloauth/` deploys the app plus a CloakBrowser sidecar.
Because `/oauth` streams SSE and a single login can take 60–120s, the chart sets
proxy config to avoid response buffering and short timeouts:

- ingress-nginx: annotations (`proxy-buffering: off`, `proxy-read-timeout: 300`).
- Gateway API / nginx-gateway-fabric: a `SnippetsFilter` (`httpRoute.sse: true`)
  injecting `proxy_buffering off; proxy_read_timeout 300s;`. Requires NGF started
  with `--snippets`.

## Release

Push a `vX.Y.Z` tag → the release workflow runs GoReleaser (multi-arch binaries
+ container image) and publishes the Helm chart to
`ghcr.io/<owner>/charts/stelloauth`. The chart's `appVersion` is set to the tag,
so the app image tag defaults to the release version.

## Gotchas / conventions

- **Synthetic input is dropped** on the Stellantis login + consent pages: CDP
  key/mouse events (`chromedp.SendKeys`/`chromedp.Click`) never reach the nodes
  (0 events fire; fields stay empty). Fill via `chromedp.Focus` + `input.InsertText`
  and submit/consent via a DOM `element.click()` (`jsClick`). Do not revert to
  SendKeys/Click. (Discovered 2026-07; plain pages are unaffected.)
- **Consent page**: post-login consent is at `id-dcr.<brand>.com/index/authorize-consentments`
  ("LOG IN COMPLETE, READY TO EXPLORE?"); the control is `#consentbutton`
  ("CONTINUE"), included in `authorizeSelectors`. It POSTs `decision=allow` to
  `/am/oauth2/authorize`, which redirects to the custom scheme carrying `code`;
  the network listener captures it (the browser can't load the scheme, so the
  page URL never changes). The consent page can render slowly (Citroen), so
  `clickAuthorizeAndWait` retries the selector scan until a deadline.
- **Per-request isolation**: pass a unique `fingerprint` (the request ID) when
  discovering the CDP URL so each request gets an isolated CloakBrowser session.
  Reusing a single shared browser wedges subsequent logins.
- **internal/app/configs.json** is the source of client IDs/secrets; keep it in sync with the
  upstream HA integration.
- Follow existing style; keep files small and focused. `golangci-lint` is the
  quality gate (errcheck, gocyclo, lll, unparam, etc.).
