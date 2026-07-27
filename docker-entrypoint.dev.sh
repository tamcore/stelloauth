#!/bin/sh
set -e

# Start the CloakBrowser CDP server in the background.
cloakserve &

# Wait for the CDP endpoint to come up.
echo "waiting for CloakBrowser CDP on :9222..."
i=0
until curl -sf http://localhost:9222/json/version >/dev/null 2>&1; do
	i=$((i + 1))
	if [ "$i" -gt 60 ]; then
		echo "CloakBrowser did not start within 60s" >&2
		exit 1
	fi
	sleep 1
done
echo "CloakBrowser is up; starting stelloauth."

exec /usr/local/bin/stelloauth
