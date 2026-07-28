package app

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
