package app

import (
	"encoding/json"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type oauthMetrics struct {
	success *prometheus.CounterVec
	failure *prometheus.CounterVec
	allowed map[string]struct{}
	gather  prometheus.Gatherer
}

func newOAuthMetrics() *oauthMetrics {
	success := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "stelloauth",
		Name:      "oauth_success_total",
		Help:      "Total number of successful Stellantis OAuth attempts.",
	}, []string{"brand", countryKey})
	failure := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "stelloauth",
		Name:      "oauth_failure_total",
		Help:      "Total number of failed Stellantis OAuth attempts.",
	}, []string{"brand", countryKey})
	registry := prometheus.NewRegistry()
	registry.MustRegister(success, failure)

	return &oauthMetrics{
		success: success,
		failure: failure,
		allowed: make(map[string]struct{}),
		gather:  registry,
	}
}

func (m *oauthMetrics) initialize(data []byte) error {
	var configs map[string]BrandConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return err
	}

	for brand, brandConfig := range configs {
		for country := range brandConfig.Configs {
			m.allowed[metricTarget(brand, country)] = struct{}{}
			m.success.WithLabelValues(brand, country).Add(0)
			m.failure.WithLabelValues(brand, country).Add(0)
		}
	}
	return nil
}

func (m *oauthMetrics) record(brand, country string, err error) {
	if _, ok := m.allowed[metricTarget(brand, country)]; !ok {
		return
	}
	if err != nil {
		m.failure.WithLabelValues(brand, country).Inc()
		return
	}
	m.success.WithLabelValues(brand, country).Inc()
}

func (m *oauthMetrics) handler() http.Handler {
	return promhttp.HandlerFor(m.gather, promhttp.HandlerOpts{})
}

func metricTarget(brand, country string) string {
	return brand + "\x00" + country
}
