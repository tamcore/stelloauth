# Stelloauth

A web-based OAuth helper for Stellantis vehicles (MyPeugeot, MyCitroën, MyDS, MyOpel, MyVauxhall).

This tool was specifically made to work with the [Home Assistant Stellantis Vehicles integration](https://github.com/andreadegiovine/homeassistant-stellantis-vehicles) and helps you obtain the OAuth authorization code required for the integration setup.

Based on the work done in [stellantis-oauth-helper](https://github.com/benbox69/stellantis-oauth-helper) by benbox69.

## Features

- ✅ Simple web interface for authentication
- ✅ Supports all Stellantis brands and countries
- ✅ Automatically fetches latest configuration
- ✅ Single binary with embedded web UI
- ✅ Docker container available

## Requirements

The application automates the OAuth flow by driving a [CloakBrowser](https://github.com/CloakHQ/CloakBrowser) stealth Chromium over the Chrome DevTools Protocol. CloakBrowser passes Stellantis' bot protection (invisible reCAPTCHA) that a plain headless Chrome cannot.

CloakBrowser runs as a separate process: a sidecar container in Kubernetes, a second service in Docker Compose, or baked into the dev image (`Dockerfile.dev`). The app connects to it via the required `CLOAK_CDP_URL` environment variable and does **not** need Chrome installed itself.

## Installation

### Helm Chart

A helm chart is available in the [charts](charts/stelloauth) directory.

```shell
# fetch available chart versions
crane ls ghcr.io/tamcore/charts/stelloauth

# deploy
helm upgrade --install \
    stelloauth \
    oci://ghcr.io/tamcore/charts/stelloauth \
    --version ${CHART_VERSION} \
    --namespace ${NAMESPACE}
```

### Docker

stelloauth needs a CloakBrowser CDP endpoint, so run both containers together:

```bash
docker run -d --name cloakbrowser cloakhq/cloakbrowser:0.5.2 cloakserve
docker run -p 8080:8080 \
  --link cloakbrowser \
  -e CLOAK_CDP_URL=http://cloakbrowser:9222 \
  ghcr.io/tamcore/stelloauth:latest
```

Then open http://localhost:8080 in your browser. For most users, Docker Compose below is simpler.

### Docker Compose

```bash
docker compose up -d
```

The compose stack runs a CloakBrowser sidecar (`cloakbrowser`) that stelloauth connects to via `CLOAK_CDP_URL`. CloakBrowser drives the login in a stealth Chromium to pass Stellantis' bot protection. The free CloakBrowser tier allows one concurrent login at a time; additional requests queue briefly. See [docker-compose.yaml](docker-compose.yaml) for the full configuration.

### Binary

Download the latest release from the [releases page](https://github.com/tamcore/stelloauth/releases) and run:

```bash
./stelloauth
```

The server starts on port 8080 by default.

**Note:** The binary requires a running CloakBrowser CDP endpoint. Start one with `docker run -p 9222:9222 cloakhq/cloakbrowser:0.5.2 cloakserve` and set `CLOAK_CDP_URL=http://localhost:9222` before launching stelloauth.

## Configuration

| Environment Variable | Default   | Description                                      |
|---------------------|-----------|--------------------------------------------------|
| `CLOAK_CDP_URL`     | *required* | CloakBrowser CDP endpoint (e.g. `http://localhost:9222`). The server exits at startup if unset. |
| `CLOAK_MAX_SESSIONS` | `1`      | Max concurrent browser sessions (CloakBrowser free tier allows 1) |
| `CLOAK_QUEUE_TIMEOUT` | `60s`   | How long a request waits for a free session before failing |
| `PORT`              | `8080`    | HTTP server port                                 |
| `HTTP_ADDRESS`      | `0.0.0.0` | Bind address                                     |
| `RATE_LIMIT_COUNT`  | -         | Max requests per IP in the rate limit window     |
| `RATE_LIMIT_DURATION` | -       | Rate limit window duration (e.g., `24h`, `1h30m`) |
| `GEOIP_COUNTRY_DB` | unset | Path or URL to a GeoLite2-Country `.mmdb`/`.mmdb.gz`; enables IP-based country pre-selection. Unset disables it. |

Rate limiting is disabled by default. Set both `RATE_LIMIT_COUNT` and `RATE_LIMIT_DURATION` to enable it.

Example with rate limiting (3 requests per 24 hours):
```bash
docker run -p 8080:8080 \
  -e CLOAK_CDP_URL=http://cloakbrowser:9222 \
  -e RATE_LIMIT_COUNT=3 \
  -e RATE_LIMIT_DURATION=24h \
  ghcr.io/tamcore/stelloauth:latest
```

## How It Works

1. Select your brand (e.g., MyPeugeot) and country
2. Enter your Stellantis account credentials
3. Click "Get OAuth Code"
4. The server automates the login flow using a CloakBrowser stealth Chromium (which passes Stellantis' bot protection)
5. Copy the OAuth code for use with your integration

Your credentials are only used to authenticate with Stellantis servers and are never stored.

## Building from Source

```bash
# Build binary
go build -o stelloauth ./cmd/stelloauth

# Run tests
go test -v ./...

# Build with goreleaser
goreleaser build --single-target --snapshot --clean

# Build Docker image locally
docker build -f Dockerfile.dev -t stelloauth .
```

## License

MIT License
