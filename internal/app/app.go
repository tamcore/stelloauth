package app

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

const (
	defaultPort           = "8080"
	defaultAddress        = "0.0.0.0"
	defaultMetricsPort    = "9090"
	defaultMetricsAddress = "0.0.0.0"
)

var applicationMetrics = newOAuthMetrics()

func Run() error {
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

	if err := applicationMetrics.initialize(configsJSON); err != nil {
		return fmt.Errorf("initialize metrics: %w", err)
	}

	appAddr, metricsAddr := serverAddresses()
	log.Printf("Starting server on %s", appAddr)
	log.Printf("Starting metrics server on %s", metricsAddr)
	return serveHTTPServers(
		appAddr,
		metricsAddr,
		newApplicationMux(),
		newMetricsMux(applicationMetrics.handler()),
	)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func serverAddresses() (string, string) {
	appAddr := net.JoinHostPort(
		getEnv("HTTP_ADDRESS", defaultAddress),
		getEnv("PORT", defaultPort),
	)
	metricsAddr := net.JoinHostPort(
		getEnv("METRICS_ADDRESS", defaultMetricsAddress),
		getEnv("METRICS_PORT", defaultMetricsPort),
	)
	return appAddr, metricsAddr
}

func serveHTTPServers(appAddr, metricsAddr string, appHandler, metricsHandler http.Handler) error {
	appListener, err := net.Listen("tcp", appAddr)
	if err != nil {
		return fmt.Errorf("application listener: %w", err)
	}

	metricsListener, err := net.Listen("tcp", metricsAddr)
	if err != nil {
		_ = appListener.Close()
		return fmt.Errorf("metrics listener: %w", err)
	}

	appServer := &http.Server{Handler: appHandler}
	metricsServer := &http.Server{Handler: metricsHandler}
	errs := make(chan error, 2)
	go func() {
		errs <- fmt.Errorf("application server: %w", appServer.Serve(appListener))
	}()
	go func() {
		errs <- fmt.Errorf("metrics server: %w", metricsServer.Serve(metricsListener))
	}()

	err = <-errs
	_ = appServer.Close()
	_ = metricsServer.Close()
	return err
}
