package admission

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hawky-4s-/k8s-ndots-admission-controller/internal/config"
)

func TestMutator_Mutate_Namespace(t *testing.T) {
	logger := slog.Default()

	tests := []struct {
		name      string
		cfg       *config.Config
		pod       *corev1.Pod
		wantPatch bool
	}{
		{
			name: "exclude namespace -> no patch",
			cfg: &config.Config{
				DNSOptions:       []config.DNSOption{{Name: "ndots", Value: "2"}},
				NamespaceExclude: []string{"kube-system"},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system"},
				Spec:       corev1.PodSpec{},
			},
			wantPatch: false,
		},
		{
			name: "include namespace (match) -> patch",
			cfg: &config.Config{
				DNSOptions:       []config.DNSOption{{Name: "ndots", Value: "2"}},
				NamespaceInclude: []string{"prod"},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "prod"},
				Spec:       corev1.PodSpec{},
			},
			wantPatch: true,
		},
		{
			name: "include namespace (no match) -> no patch",
			cfg: &config.Config{
				DNSOptions:       []config.DNSOption{{Name: "ndots", Value: "2"}},
				NamespaceInclude: []string{"prod"},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "dev"},
				Spec:       corev1.PodSpec{},
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
				assert.NotEmpty(t, patches, "expected patch but got none")
			} else {
				assert.Empty(t, patches, "expected no patch but got one")
			}
		})
	}
}
