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

The application uses headless Chrome (via chromedp) to automate the OAuth flow. Chrome/Chromium must be installed on the system.

When running in Docker, Chrome is included in the container image.

## Usage

### Docker (Recommended)

```bash
docker run -p 8080:8080 ghcr.io/tamcore/stelloauth:latest
```

Then open http://localhost:8080 in your browser.

### Docker Compose

```bash
docker compose up -d
```

See [docker-compose.yaml](docker-compose.yaml) for the example configuration.

### Binary

Download the latest release from the [releases page](https://github.com/tamcore/stelloauth/releases) and run:

```bash
./stelloauth
```

The server starts on port 8080 by default.

**Note:** You need Chrome/Chromium installed on your system for the binary to work.

## Configuration

| Environment Variable | Default   | Description                                      |
|---------------------|-----------|--------------------------------------------------|
| `PORT`              | `8080`    | HTTP server port                                 |
| `HTTP_ADDRESS`      | `0.0.0.0` | Bind address                                     |
| `RATE_LIMIT_COUNT`  | -         | Max requests per IP in the rate limit window     |
| `RATE_LIMIT_DURATION` | -       | Rate limit window duration (e.g., `24h`, `1h30m`) |

Rate limiting is disabled by default. Set both `RATE_LIMIT_COUNT` and `RATE_LIMIT_DURATION` to enable it.

Example with rate limiting (3 requests per 24 hours):
```bash
docker run -p 8080:8080 \
  -e RATE_LIMIT_COUNT=3 \
  -e RATE_LIMIT_DURATION=24h \
  ghcr.io/tamcore/stelloauth:latest
```

## How It Works

1. Select your brand (e.g., MyPeugeot) and country
2. Enter your Stellantis account credentials
3. Click "Get OAuth Code"
4. The server automates the login flow using headless Chrome
5. Copy the OAuth code for use with your integration

Your credentials are only used to authenticate with Stellantis servers and are never stored.

## Building from Source

```bash
# Build binary
go build -o stelloauth .

# Run tests
go test -v ./...

# Build with goreleaser
goreleaser build --single-target --snapshot --clean
```

## License

MIT License
