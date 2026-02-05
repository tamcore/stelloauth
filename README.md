# Stelloauth

A web-based OAuth helper for Stellantis vehicles (MyPeugeot, MyCitroën, MyDS, MyOpel, MyVauxhall).

This tool helps you obtain the OAuth authorization code required for vehicle integrations like the [Home Assistant Stellantis integration](https://github.com/andreadegiovine/homeassistant-stellantis-vehicles).

## Features

- ✅ Simple web interface for authentication
- ✅ Supports all Stellantis brands and countries
- ✅ Automatically fetches latest configuration
- ✅ Single binary with embedded web UI
- ✅ Docker container available

## Usage

### Docker (Recommended)

```bash
docker run -p 8080:8080 ghcr.io/tamcore/stelloauth:latest
```

Then open http://localhost:8080 in your browser.

### Binary

Download the latest release from the [releases page](https://github.com/tamcore/stelloauth/releases) and run:

```bash
./stelloauth
```

The server starts on port 8080 by default.

## Configuration

| Environment Variable | Default   | Description              |
|---------------------|-----------|--------------------------|
| `PORT`              | `8080`    | HTTP server port         |
| `HTTP_ADDRESS`      | `0.0.0.0` | Bind address             |

## How It Works

1. Select your brand (e.g., MyPeugeot) and country
2. Enter your Stellantis account credentials
3. Click "Get OAuth Code"
4. Copy the OAuth code for use with your integration

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
