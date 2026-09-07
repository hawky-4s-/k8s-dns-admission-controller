package admission

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hawky-4s-/k8s-dns-admission-controller/internal/config"
)

func TestMutator_Mutate_Annotations(t *testing.T) {
	logger := slog.Default()

	tests := []struct {
		name      string
		cfg       *config.Config
		pod       *corev1.Pod
		wantPatch bool
	}{
		{
			name: "opt-in mode: no annotation -> no patch",
			cfg: &config.Config{
				DNSOptions:             []config.DNSOption{{Name: "ndots", Value: "2"}},
				DNSAnnotationMode:      "opt-in",
				DNSEnableAnnotationKey: "dns.hawky.dev/dns",
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "pod"},
				Spec:       corev1.PodSpec{DNSConfig: &corev1.PodDNSConfig{}},
			},
			wantPatch: false,
		},
		{
			name: "opt-in mode: with annotation -> patch",
			cfg: &config.Config{
				DNSOptions:             []config.DNSOption{{Name: "ndots", Value: "2"}},
				DNSAnnotationMode:      "opt-in",
				DNSEnableAnnotationKey: "dns.hawky.dev/dns",
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "pod",
					Annotations: map[string]string{"dns.hawky.dev/dns": "true"},
				},
				Spec: corev1.PodSpec{DNSConfig: &corev1.PodDNSConfig{}},
			},
			wantPatch: true,
		},
		{
			name: "opt-out mode: false annotation -> no patch",
			cfg: &config.Config{
				DNSOptions:             []config.DNSOption{{Name: "ndots", Value: "2"}},
				DNSAnnotationMode:      "opt-out",
				DNSEnableAnnotationKey: "dns.hawky.dev/dns",
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "pod",
					Annotations: map[string]string{"dns.hawky.dev/dns": "false"},
				},
				Spec: corev1.PodSpec{DNSConfig: &corev1.PodDNSConfig{}},
			},
			wantPatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutator := NewMutator(tt.cfg, logger)
			patches, err := mutator.Mutate(tt.pod)
			require.NoError(t, err)

			if tt.wantPatch {
				assert.NotEmpty(t, patches)
			} else {
				assert.Empty(t, patches)
			}
		})
	}
}
