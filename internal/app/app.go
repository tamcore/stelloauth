package app

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

const (
	defaultPort    = "8080"
	defaultAddress = "0.0.0.0"
)

func Run() error {
	port := getEnv("PORT", defaultPort)
	address := getEnv("HTTP_ADDRESS", defaultAddress)

	if os.Getenv("CLOAK_CDP_URL") == "" {
		return errors.New(
			"CLOAK_CDP_URL is required: set it to the CloakBrowser CDP endpoint (e.g. http://localhost:9222)",
		)
	}
	sessionGate = newSessionGate(
		getIntEnv("CLOAK_MAX_SESSIONS", 1),
		getDurationEnv("CLOAK_QUEUE_TIMEOUT", 60*time.Second),
	)

	if db, err := loadCountryDB(os.Getenv("GEOIP_COUNTRY_DB")); err != nil {
		log.Printf("GeoIP country pre-selection disabled: %v", err)
	} else if db != nil {
		countryDB = db
		log.Printf("GeoIP country pre-selection enabled")
	}

	initRateLimiter()

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/configs", handleConfigs)
	http.HandleFunc("/geo", handleGeo)
	http.HandleFunc("/oauth", handleOAuth)
	http.HandleFunc("/worker", handleWorker)

	addr := fmt.Sprintf("%s:%s", address, port)
	log.Printf("Starting server on %s", addr)
	return http.ListenAndServe(addr, nil)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
