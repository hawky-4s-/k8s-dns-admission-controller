package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	// Clean env
	os.Clearenv()

	t.Run("defaults", func(t *testing.T) {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 8443, cfg.Port)
		assert.Equal(t, 2, cfg.NdotsValue)
		assert.Equal(t, "change-ndots", cfg.AnnotationKey)
		assert.Equal(t, "opt-out", cfg.AnnotationMode)
		assert.Len(t, cfg.NamespaceExclude, 3) // kube-system, kube-public, kube-node-lease
		assert.Equal(t, 10*time.Second, cfg.Timeout)
		// New fields
		assert.Equal(t, "info", cfg.LogLevel)
		assert.Equal(t, "json", cfg.LogFormat)
		assert.Equal(t, 8080, cfg.MetricsPort)
		// DNS defaults
		assert.Equal(t, "merge", cfg.DNSStrategy)
		assert.Equal(t, "ndots.hawky.dev/dns-config", cfg.SpecAnnotationKey)
		assert.Equal(t, "ndots.hawky.dev/dns-strategy", cfg.StrategyAnnotationKey)
		assert.Empty(t, cfg.DNSNameservers)
		assert.Empty(t, cfg.DNSSearches)
		assert.Empty(t, cfg.DNSOptions)
		assert.Empty(t, cfg.DNSPolicy)
	})

	t.Run("from env", func(t *testing.T) {
		require.NoError(t, os.Setenv("PORT", "9090"))
		require.NoError(t, os.Setenv("NDOTS_VALUE", "5"))
		require.NoError(t, os.Setenv("ANNOTATION_MODE", "opt-in"))
		require.NoError(t, os.Setenv("NAMESPACE_INCLUDE", "prod,staging"))
		require.NoError(t, os.Setenv("LOG_LEVEL", "debug"))
		require.NoError(t, os.Setenv("LOG_FORMAT", "text"))
		require.NoError(t, os.Setenv("METRICS_PORT", "9090"))

		defer os.Clearenv()

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 9090, cfg.Port)
		assert.Equal(t, 5, cfg.NdotsValue)
		assert.Equal(t, "opt-in", cfg.AnnotationMode)
		assert.Equal(t, []string{"prod", "staging"}, cfg.NamespaceInclude)
		assert.Equal(t, "debug", cfg.LogLevel)
		assert.Equal(t, "text", cfg.LogFormat)
		assert.Equal(t, 9090, cfg.MetricsPort)
	})

	t.Run("dns from env", func(t *testing.T) {
		require.NoError(t, os.Setenv("DNS_NAMESERVERS", "1.1.1.1, 8.8.8.8"))
		require.NoError(t, os.Setenv("DNS_SEARCHES", "svc.cluster.local"))
		require.NoError(t, os.Setenv("DNS_OPTIONS", "ndots=3,edns0="))
		require.NoError(t, os.Setenv("DNS_POLICY", "None"))
		require.NoError(t, os.Setenv("DNS_STRATEGY", "override"))
		require.NoError(t, os.Setenv("DNS_SPEC_ANNOTATION_KEY", "custom/dns"))
		require.NoError(t, os.Setenv("DNS_STRATEGY_ANNOTATION_KEY", "custom/strategy"))

		defer os.Clearenv()

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, []string{"1.1.1.1", "8.8.8.8"}, cfg.DNSNameservers)
		assert.Equal(t, []string{"svc.cluster.local"}, cfg.DNSSearches)
		require.Len(t, cfg.DNSOptions, 2)
		assert.Equal(t, DNSOption{Name: "ndots", Value: "3"}, cfg.DNSOptions[0])
		assert.Equal(t, DNSOption{Name: "edns0", Value: ""}, cfg.DNSOptions[1])
		assert.Equal(t, "None", cfg.DNSPolicy)
		assert.Equal(t, "override", cfg.DNSStrategy)
		assert.Equal(t, "custom/dns", cfg.SpecAnnotationKey)
		assert.Equal(t, "custom/strategy", cfg.StrategyAnnotationKey)
	})

	t.Run("bad env", func(t *testing.T) {
		require.NoError(t, os.Setenv("PORT", "invalid"))
		defer os.Clearenv()

		cfg, err := Load()
		// If int parsing fails, we usually ignore or error. Plan didn't specify strict fail on parse, but Config.Load logic usually implies it behaves.
		// If Load ignores errors (as mostly using strconv.AtoI and ignoring err in snippet), then it keeps default.
		// Wait, snippet said "if port, err := strconv.Atoi(v); err == nil". So it ignores invalid ints.
		require.NoError(t, err)
		assert.Equal(t, 8443, cfg.Port) // Default remains
	})
}

func TestConfig_Validate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		cfg := DefaultConfig
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("invalid port", func(t *testing.T) {
		cfg := DefaultConfig
		cfg.Port = 90000
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "port")
	})

	t.Run("invalid ndots", func(t *testing.T) {
		cfg := DefaultConfig
		cfg.NdotsValue = 16
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ndots")
	})

	t.Run("invalid annot mode", func(t *testing.T) {
		cfg := DefaultConfig
		cfg.AnnotationMode = "foo"
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "annotationMode")
	})

	t.Run("invalid dns strategy", func(t *testing.T) {
		cfg := DefaultConfig
		cfg.DNSStrategy = "bogus"
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "dnsStrategy")
	})

	t.Run("invalid dns policy", func(t *testing.T) {
		cfg := DefaultConfig
		cfg.DNSPolicy = "Nonsense"
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "dnsPolicy")
	})

	t.Run("valid dns policy accepted", func(t *testing.T) {
		cfg := DefaultConfig
		cfg.DNSPolicy = "None"
		cfg.DNSNameservers = []string{"1.1.1.1"}
		err := cfg.Validate()
		assert.NoError(t, err)
	})

	t.Run("empty dns option name", func(t *testing.T) {
		cfg := DefaultConfig
		cfg.DNSOptions = []DNSOption{{Name: "", Value: "x"}}
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "option")
	})
}
