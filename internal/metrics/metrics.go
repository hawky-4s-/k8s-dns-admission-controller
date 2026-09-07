// Package metrics provides Prometheus metrics for the webhook.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "dns_webhook"
)

// Recorder records webhook metrics to Prometheus.
type Recorder struct {
	mutationsTotal  *prometheus.CounterVec
	errorsTotal     *prometheus.CounterVec
	requestDuration prometheus.Histogram
}

// NewRecorder creates a new metrics Recorder and registers metrics with the given registry.
func NewRecorder(reg prometheus.Registerer) *Recorder {
	r := &Recorder{
		mutationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "mutations_total",
				Help:      "Total number of pod mutations processed",
			},
			[]string{"namespace", "action"},
		),
		errorsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "errors_total",
				Help:      "Total number of errors during admission processing",
			},
			[]string{"type"},
		),
		requestDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "request_duration_seconds",
				Help:      "Duration of admission requests in seconds",
				Buckets:   prometheus.DefBuckets,
			},
		),
	}

	reg.MustRegister(r.mutationsTotal)
	reg.MustRegister(r.errorsTotal)
	reg.MustRegister(r.requestDuration)

	return r
}

// RecordMutation records a mutation event.
// action should be "mutated" or "skipped".
func (r *Recorder) RecordMutation(namespace, action string) {
	r.mutationsTotal.WithLabelValues(namespace, action).Inc()
}

// RecordError records an error event.
// errorType should be "decode", "mutation", or "marshal".
func (r *Recorder) RecordError(errorType string) {
	r.errorsTotal.WithLabelValues(errorType).Inc()
}

// ObserveRequestDuration records the duration of a request.
func (r *Recorder) ObserveRequestDuration(seconds float64) {
	r.requestDuration.Observe(seconds)
}
