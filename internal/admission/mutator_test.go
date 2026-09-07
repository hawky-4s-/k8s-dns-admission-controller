package admission

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/hawky-4s-/k8s-dns-admission-controller/internal/config"
)

func TestMutator_Mutate(t *testing.T) {
	logger := slog.Default()
	// ndots is now an ordinary configured option; there is no auto-seeding.
	cfg := &config.Config{
		DNSOptions:  []config.DNSOption{{Name: "ndots", Value: "2"}},
		DNSStrategy: "merge",
	}
	mutator := NewMutator(cfg, logger)

	ndotsTwo := "2"
	ndotsFive := "5"

	tests := []struct {
		name         string
		pod          *corev1.Pod
		wantPatchLen int
		wantOp       string
		wantPath     string
		wantValCheck func(*testing.T, interface{})
	}{
		{
			name:         "no dnsConfig -> add full config",
			pod:          &corev1.Pod{Spec: corev1.PodSpec{}},
			wantPatchLen: 1,
			wantOp:       "add",
			wantPath:     "/spec/dnsConfig",
			wantValCheck: func(t *testing.T, res interface{}) {
				// Assert specific structure of the value map
				val, ok := res.(map[string]interface{})
				require.True(t, ok)
				opts, ok := val["options"].([]map[string]interface{})
				require.True(t, ok)
				require.Len(t, opts, 1)
				assert.Equal(t, "ndots", opts[0]["name"])
				assert.Equal(t, "2", opts[0]["value"])
			},
		},
		{
			name: "empty options -> add options array",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					DNSConfig: &corev1.PodDNSConfig{},
				},
			},
			wantPatchLen: 1,
			wantOp:       "add",
			wantPath:     "/spec/dnsConfig/options",
			wantValCheck: func(t *testing.T, res interface{}) {
				val, ok := res.([]map[string]interface{})
				require.True(t, ok)
				require.Len(t, val, 1)
				assert.Equal(t, "ndots", val[0]["name"])
				assert.Equal(t, "2", val[0]["value"])
			},
		},
		{
			name: "options without ndots -> append ndots",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					DNSConfig: &corev1.PodDNSConfig{
						Options: []corev1.PodDNSConfigOption{
							{Name: "timeout", Value: nil},
						},
					},
				},
			},
			wantPatchLen: 1,
			wantOp:       "add",
			wantPath:     "/spec/dnsConfig/options/-",
			wantValCheck: func(t *testing.T, res interface{}) {
				val, ok := res.(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, "ndots", val["name"])
				assert.Equal(t, "2", val["value"])
			},
		},
		{
			name: "ndots with wrong value -> replace",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					DNSConfig: &corev1.PodDNSConfig{
						Options: []corev1.PodDNSConfigOption{
							{Name: "ndots", Value: &ndotsFive},
						},
					},
				},
			},
			wantPatchLen: 1,
			wantOp:       "replace",
			wantPath:     "/spec/dnsConfig/options/0/value",
			wantValCheck: func(t *testing.T, res interface{}) {
				val, ok := res.(string)
				require.True(t, ok)
				assert.Equal(t, "2", val)
			},
		},
		{
			name: "ndots correct -> no patch",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					DNSConfig: &corev1.PodDNSConfig{
						Options: []corev1.PodDNSConfigOption{
							{Name: "ndots", Value: &ndotsTwo},
						},
					},
				},
			},
			wantPatchLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patches, err := mutator.Mutate(tt.pod)
			require.NoError(t, err)
			require.Len(t, patches, tt.wantPatchLen)

			if tt.wantPatchLen > 0 {
				patch := patches[0]
				assert.Equal(t, tt.wantOp, patch.Op)
				assert.Equal(t, tt.wantPath, patch.Path)
				if tt.wantValCheck != nil {
					tt.wantValCheck(t, patch.Value)
				}
			}
		})
	}
}

// TestMutator_Mutate_NoDNSConfigIsNoOp verifies the no-op default: with no DNS
// settings configured, the webhook emits no patches for any pod.
func TestMutator_Mutate_NoOpDefault(t *testing.T) {
	logger := slog.Default()
	mutator := NewMutator(&config.Config{DNSStrategy: "merge"}, logger)

	pods := []*corev1.Pod{
		{Spec: corev1.PodSpec{}},
		{Spec: corev1.PodSpec{DNSConfig: &corev1.PodDNSConfig{}}},
		{Spec: corev1.PodSpec{DNSConfig: &corev1.PodDNSConfig{
			Options: []corev1.PodDNSConfigOption{{Name: "ndots", Value: strPtr("5")}},
		}}},
	}
	for _, pod := range pods {
		patches, err := mutator.Mutate(pod)
		require.NoError(t, err)
		assert.Empty(t, patches, "empty config must produce no patches")
	}
}
