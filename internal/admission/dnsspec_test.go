package admission

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/hawky-4s-/k8s-ndots-admission-controller/internal/config"
)

func strPtr(s string) *string { return &s }

func TestParseStrategy(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Strategy
		wantErr bool
	}{
		{"empty defaults to merge", "", StrategyMerge, false},
		{"merge", "merge", StrategyMerge, false},
		{"uppercase merge", "MERGE", StrategyMerge, false},
		{"update", "update", StrategyUpdate, false},
		{"unset", "unset", StrategyUnset, false},
		{"override", "override", StrategyOverride, false},
		{"whitespace trimmed", "  merge  ", StrategyMerge, false},
		{"invalid", "bogus", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStrategy(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseDNSSpecAnnotation_JSON(t *testing.T) {
	raw := `{
		"nameservers": ["1.1.1.1", "8.8.8.8"],
		"searches": ["svc.cluster.local"],
		"options": [{"name": "ndots", "value": "3"}, {"name": "edns0"}],
		"dnsPolicy": "None",
		"strategy": "override"
	}`

	spec, err := ParseDNSSpecAnnotation(raw)
	require.NoError(t, err)

	assert.Equal(t, []string{"1.1.1.1", "8.8.8.8"}, spec.Nameservers)
	assert.Equal(t, []string{"svc.cluster.local"}, spec.Searches)
	require.Len(t, spec.Options, 2)
	assert.Equal(t, "ndots", spec.Options[0].Name)
	require.NotNil(t, spec.Options[0].Value)
	assert.Equal(t, "3", *spec.Options[0].Value)
	assert.Equal(t, "edns0", spec.Options[1].Name)
	assert.Nil(t, spec.Options[1].Value)
	require.NotNil(t, spec.DNSPolicy)
	assert.Equal(t, corev1.DNSNone, *spec.DNSPolicy)
	assert.Equal(t, StrategyOverride, spec.Strategy)
}

func TestParseDNSSpecAnnotation_YAML(t *testing.T) {
	raw := `
nameservers:
  - 1.1.1.1
  - 8.8.8.8
searches:
  - svc.cluster.local
options:
  - name: ndots
    value: "3"
  - name: edns0
dnsPolicy: None
strategy: override
`

	spec, err := ParseDNSSpecAnnotation(raw)
	require.NoError(t, err)

	assert.Equal(t, []string{"1.1.1.1", "8.8.8.8"}, spec.Nameservers)
	assert.Equal(t, []string{"svc.cluster.local"}, spec.Searches)
	require.Len(t, spec.Options, 2)
	assert.Equal(t, "3", *spec.Options[0].Value)
	require.NotNil(t, spec.DNSPolicy)
	assert.Equal(t, corev1.DNSNone, *spec.DNSPolicy)
	assert.Equal(t, StrategyOverride, spec.Strategy)
}

func TestParseDNSSpecAnnotation_Invalid(t *testing.T) {
	_, err := ParseDNSSpecAnnotation("{not: valid: yaml: [")
	require.Error(t, err)
}

func TestParseDNSSpecAnnotation_InvalidStrategy(t *testing.T) {
	_, err := ParseDNSSpecAnnotation(`{"strategy": "bogus"}`)
	require.Error(t, err)
}

func TestDNSSpec_MergeOverlay(t *testing.T) {
	base := DNSSpec{
		Nameservers: []string{"10.0.0.10"},
		Searches:    []string{"base.local"},
		Options: []corev1.PodDNSConfigOption{
			{Name: "ndots", Value: strPtr("2")},
		},
		Strategy: StrategyMerge,
	}

	t.Run("searches-only overlay preserves base options and nameservers", func(t *testing.T) {
		overlay := DNSSpec{Searches: []string{"over.local"}}
		got := base.MergeOverlay(overlay)
		assert.Equal(t, []string{"over.local"}, got.Searches)
		assert.Equal(t, []string{"10.0.0.10"}, got.Nameservers)
		require.Len(t, got.Options, 1)
		assert.Equal(t, "ndots", got.Options[0].Name)
	})

	t.Run("option replace by name", func(t *testing.T) {
		overlay := DNSSpec{Options: []corev1.PodDNSConfigOption{{Name: "ndots", Value: strPtr("5")}}}
		got := base.MergeOverlay(overlay)
		require.Len(t, got.Options, 1)
		assert.Equal(t, "5", *got.Options[0].Value)
	})

	t.Run("option append preserves both", func(t *testing.T) {
		overlay := DNSSpec{Options: []corev1.PodDNSConfigOption{{Name: "edns0"}}}
		got := base.MergeOverlay(overlay)
		require.Len(t, got.Options, 2)
		names := []string{got.Options[0].Name, got.Options[1].Name}
		assert.ElementsMatch(t, []string{"ndots", "edns0"}, names)
	})

	t.Run("overlay strategy replaces base", func(t *testing.T) {
		overlay := DNSSpec{Strategy: StrategyOverride}
		got := base.MergeOverlay(overlay)
		assert.Equal(t, StrategyOverride, got.Strategy)
	})
}

func TestDefaultDNSSpecFromConfig(t *testing.T) {
	t.Run("ndots only maps to single option, merge strategy", func(t *testing.T) {
		cfg := &config.Config{NdotsValue: 2, DNSStrategy: "merge"}
		spec := DefaultDNSSpecFromConfig(cfg)
		require.Len(t, spec.Options, 1)
		assert.Equal(t, "ndots", spec.Options[0].Name)
		require.NotNil(t, spec.Options[0].Value)
		assert.Equal(t, "2", *spec.Options[0].Value)
		assert.Equal(t, StrategyMerge, spec.Strategy)
		assert.Empty(t, spec.Nameservers)
		assert.Empty(t, spec.Searches)
		assert.Nil(t, spec.DNSPolicy)
	})

	t.Run("nameservers searches policy mapped through", func(t *testing.T) {
		cfg := &config.Config{
			NdotsValue:     2,
			DNSStrategy:    "override",
			DNSNameservers: []string{"10.0.0.10"},
			DNSSearches:    []string{"svc.local"},
			DNSPolicy:      "None",
		}
		spec := DefaultDNSSpecFromConfig(cfg)
		assert.Equal(t, []string{"10.0.0.10"}, spec.Nameservers)
		assert.Equal(t, []string{"svc.local"}, spec.Searches)
		require.NotNil(t, spec.DNSPolicy)
		assert.Equal(t, corev1.DNSNone, *spec.DNSPolicy)
		assert.Equal(t, StrategyOverride, spec.Strategy)
	})

	t.Run("config options merge with ndots", func(t *testing.T) {
		cfg := &config.Config{
			NdotsValue:  2,
			DNSStrategy: "merge",
			DNSOptions:  []config.DNSOption{{Name: "edns0", Value: ""}},
		}
		spec := DefaultDNSSpecFromConfig(cfg)
		names := make([]string, 0, len(spec.Options))
		for _, o := range spec.Options {
			names = append(names, o.Name)
		}
		assert.ElementsMatch(t, []string{"ndots", "edns0"}, names)
	})
}
