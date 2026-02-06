#!/usr/bin/env bash
set -euo pipefail

# E2E test suite for stelloauth
# Requires environment variables in format: {BRAND}_{COUNTRY}_USERNAME and {BRAND}_{COUNTRY}_PASSWORD
# Example: OPEL_DE_USERNAME, OPEL_DE_PASSWORD
#
# Usage: ./e2e_test.sh [BRAND]
#   BRAND: Optional filter (OPEL, PEUGEOT, CITROEN, MYDS, VAUXHALL)
#   If not provided, runs all configured tests

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
PORT="${PORT:-18080}"
BASE_URL="http://localhost:${PORT}"
PID_FILE="/tmp/stelloauth-e2e.pid"
BRAND_FILTER="${1:-}"

# Brand mapping from env var prefix to API brand name
declare -A BRAND_MAP=(
    ["OPEL"]="MyOpel"
    ["PEUGEOT"]="MyPeugeot"
    ["CITROEN"]="MyCitroen"
    ["MYDS"]="MyDS"
    ["VAUXHALL"]="MyVauxhall"
)

cleanup() {
    echo "Cleaning up..."
    if [[ -f "$PID_FILE" ]]; then
        kill "$(cat "$PID_FILE")" 2>/dev/null || true
        rm -f "$PID_FILE"
    fi
}

trap cleanup EXIT

start_server() {
    echo "Building stelloauth..."
    cd "$PROJECT_DIR"
    go build -o stelloauth .

    echo "Starting stelloauth on port $PORT..."
    PORT="$PORT" ./stelloauth &
    echo $! > "$PID_FILE"

    # Wait for server to be ready
    for i in {1..30}; do
        if curl -s "${BASE_URL}/" > /dev/null 2>&1; then
            echo "Server is ready"
            return 0
        fi
        sleep 0.5
    done
    echo "ERROR: Server failed to start"
    exit 1
}

# Extract credentials from environment and run test
run_test() {
    local env_prefix="$1"
    local country="$2"
    local username_var="${env_prefix}_${country}_USERNAME"
    local password_var="${env_prefix}_${country}_PASSWORD"

    local username="${!username_var:-}"
    local password="${!password_var:-}"

    if [[ -z "$username" || -z "$password" ]]; then
        echo "SKIP: ${env_prefix}_${country} - credentials not set"
        return 0
    fi

    local brand="${BRAND_MAP[$env_prefix]:-}"
    if [[ -z "$brand" ]]; then
        echo "ERROR: Unknown brand prefix: $env_prefix"
        return 1
    fi

    # Country code is uppercase in API
    local country_upper="${country^^}"

    echo "TEST: $brand / $country_upper ($username)"

    local response
    local http_code
    response=$(curl -s -w "\n%{http_code}" -X POST "${BASE_URL}/oauth" \
        -H "Content-Type: application/json" \
        -d "{\"brand\":\"$brand\",\"country\":\"$country_upper\",\"email\":\"$username\",\"password\":\"$password\"}")

    http_code=$(echo "$response" | tail -n1)
    local body
    body=$(echo "$response" | sed '$d')

    if [[ "$http_code" == "200" ]]; then
        local status
        status=$(echo "$body" | jq -r '.status // empty')
        if [[ "$status" == "success" ]]; then
            local code
            code=$(echo "$body" | jq -r '.data.code // empty')
            if [[ -n "$code" ]]; then
                echo "  PASS: Got OAuth code (${#code} chars)"
                return 0
            fi
        fi
    fi

    echo "  FAIL: HTTP $http_code"
    echo "  Response: $body"
    return 1
}

# Discover and run all configured tests
run_all_tests() {
    local passed=0
    local failed=0
    local skipped=0

    # Collect all test cases first
    local test_cases=()
    for env_prefix in "${!BRAND_MAP[@]}"; do
        # Skip if brand filter is set and doesn't match
        if [[ -n "$BRAND_FILTER" && "${env_prefix^^}" != "${BRAND_FILTER^^}" ]]; then
            continue
        fi

        while IFS= read -r var; do
            if [[ "$var" =~ ^${env_prefix}_([A-Z]+)_USERNAME$ ]]; then
                local country="${BASH_REMATCH[1]}"
                test_cases+=("${env_prefix}:${country}")
            fi
        done < <(env | grep "^${env_prefix}_" | cut -d= -f1 | sort -u)
    done

    if [[ ${#test_cases[@]} -eq 0 ]]; then
        if [[ -n "$BRAND_FILTER" ]]; then
            echo "ERROR: No credentials found for brand: $BRAND_FILTER"
            echo "Expected: ${BRAND_FILTER^^}_<COUNTRY>_USERNAME and ${BRAND_FILTER^^}_<COUNTRY>_PASSWORD"
        else
            echo "ERROR: No credentials found in environment"
        fi
        return 1
    fi

    # Run each test
    for test_case in "${test_cases[@]}"; do
        local env_prefix="${test_case%%:*}"
        local country="${test_case##*:}"
        local username_var="${env_prefix}_${country}_USERNAME"

        if run_test "$env_prefix" "$country"; then
            if [[ -z "${!username_var:-}" ]]; then
                ((skipped++)) || true
            else
                ((passed++)) || true
            fi
        else
            ((failed++)) || true
        fi
    done

    echo ""
    echo "========================================"
    echo "Results: $passed passed, $failed failed, $skipped skipped"
    echo "========================================"

    [[ $failed -eq 0 ]]
}

main() {
    echo "========================================"
    echo "Stelloauth E2E Test Suite"
    if [[ -n "$BRAND_FILTER" ]]; then
        echo "Filter: ${BRAND_FILTER^^}"
    fi
    echo "========================================"
    echo ""

    start_server
    echo ""
    run_all_tests
}

main "$@"
