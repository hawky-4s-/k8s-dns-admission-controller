package config

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConfig_LogValue(t *testing.T) {
	// Create a fully populated config
	cfg := &Config{
		NdotsValue:            2,
		AnnotationKey:         "test-key",
		AnnotationMode:        "opt-in",
		NamespaceInclude:      []string{"inc1", "inc2"},
		NamespaceExclude:      []string{"exc1"},
		Port:                  8443,
		TLSCertPath:           "/path/to/cert",
		TLSKeyPath:            "/path/to/key",
		Timeout:               5 * time.Second,
		LogLevel:              "debug",
		LogFormat:             "json",
		MetricsPort:           9090,
		DNSNameservers:        []string{"1.1.1.1"},
		DNSSearches:           []string{"svc.local"},
		DNSOptions:            []DNSOption{{Name: "ndots", Value: "2"}},
		DNSPolicy:             "None",
		DNSStrategy:           "override",
		SpecAnnotationKey:     "custom/dns",
		StrategyAnnotationKey: "custom/strategy",
	}

	// Verify it implements LogValuer
	var _ slog.LogValuer = cfg

	// Get the log value
	val := cfg.LogValue()

	// Assert it's a group
	assert.Equal(t, slog.KindGroup, val.Kind())

	// Convert group to map for easier assertion
	attrs := val.Group()
	attrMap := make(map[string]slog.Value)
	for _, a := range attrs {
		attrMap[a.Key] = a.Value
	}

	// Assert fields
	assert.Equal(t, int64(2), attrMap["ndotsValue"].Int64())
	assert.Equal(t, "test-key", attrMap["annotationKey"].String())
	assert.Equal(t, "opt-in", attrMap["annotationMode"].String())

	// Slices are a bit tricky in slog.Value, usually Any.
	// We can check they exist.
	assert.NotNil(t, attrMap["namespaceInclude"])
	assert.NotNil(t, attrMap["namespaceExclude"])

	assert.Equal(t, int64(8443), attrMap["port"].Int64())
	assert.Equal(t, "/path/to/cert", attrMap["tlsCertPath"].String())
	assert.Equal(t, "/path/to/key", attrMap["tlsKeyPath"].String())

	// Timeout should be string "5s"
	assert.Equal(t, "5s", attrMap["timeout"].String())

	assert.Equal(t, "debug", attrMap["logLevel"].String())
	assert.Equal(t, "json", attrMap["logFormat"].String())
	assert.Equal(t, int64(9090), attrMap["metricsPort"].Int64())

	// DNS fields
	assert.NotNil(t, attrMap["dnsNameservers"])
	assert.NotNil(t, attrMap["dnsSearches"])
	assert.NotNil(t, attrMap["dnsOptions"])
	assert.Equal(t, "None", attrMap["dnsPolicy"].String())
	assert.Equal(t, "override", attrMap["dnsStrategy"].String())
	assert.Equal(t, "custom/dns", attrMap["specAnnotationKey"].String())
	assert.Equal(t, "custom/strategy", attrMap["strategyAnnotationKey"].String())
}
