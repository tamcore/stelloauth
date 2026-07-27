# stelloauth — agent guide

Web tool that obtains a Stellantis OAuth authorization code for the
[Home Assistant Stellantis Vehicles integration](https://github.com/andreadegiovine/homeassistant-stellantis-vehicles).
Single Go binary with an embedded web UI. It drives the vendor login flow in a
browser and captures the `code` from the custom-scheme redirect.

## Architecture

- **App** (`main.go`): HTTP server (`/`, `/configs`, `/oauth`). `/oauth` accepts
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
| `main.go` | HTTP handlers, OAuth/chromedp flow, progress heartbeat |
| `browser.go` | Discover the CloakBrowser CDP websocket URL |
| `session.go` | Concurrency gate (bounded browser sessions) |
| `config.go` | Env parsing helpers, `sessionGate` |
| `configs.json` | Embedded brand/country OAuth config (mirrors the upstream HA integration's `configs.json`) |
| `web/index.html` | Embedded UI |
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

## Common commands

```bash
go build ./...
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
go run .
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

- **Credential entry**: use `chromedp.SendKeys` (real key events — the login and
  bot scoring need genuine input). Do **not** `Click` the field first; it can
  fail "not focusable" while the login form is still initializing. `SendKeys`
  focuses the node itself.
- **Consent page**: the post-login authorization control is `#consentbutton`
  ("CONTINUE"); it is included in `authorizeSelectors`.
- **Per-request isolation**: pass a unique `fingerprint` (the request ID) when
  discovering the CDP URL so each request gets an isolated CloakBrowser session.
  Reusing a single shared browser wedges subsequent logins.
- **configs.json** is the source of client IDs/secrets; keep it in sync with the
  upstream HA integration.
- Follow existing style; keep files small and focused. `golangci-lint` is the
  quality gate (errcheck, gocyclo, lll, unparam, etc.).
