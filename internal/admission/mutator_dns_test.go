package admission

import (
	"encoding/json"
	"log/slog"
	"testing"

	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/hawky-4s-/k8s-ndots-admission-controller/internal/config"
)

// applyPatches applies the mutator's JSON Patch to the pod and returns the
// resulting pod. This validates that the patch is well-formed and lands where
// intended, rather than asserting on op strings.
func applyPatches(t *testing.T, pod *corev1.Pod, patches []PatchOperation) *corev1.Pod {
	t.Helper()

	podJSON, err := json.Marshal(pod)
	require.NoError(t, err)

	patchJSON, err := json.Marshal(patches)
	require.NoError(t, err)

	decoded, err := jsonpatch.DecodePatch(patchJSON)
	require.NoError(t, err, "patch must be a valid JSON Patch")

	resultJSON, err := decoded.Apply(podJSON)
	require.NoError(t, err, "patch must apply cleanly to the pod")

	var result corev1.Pod
	require.NoError(t, json.Unmarshal(resultJSON, &result))
	return &result
}

func newMutatorWithSpec(t *testing.T, spec DNSSpec) *Mutator {
	t.Helper()
	cfg := &config.Config{
		NdotsValue:            2,
		AnnotationMode:        "always",
		SpecAnnotationKey:     "ndots.hawky.dev/dns-config",
		StrategyAnnotationKey: "ndots.hawky.dev/dns-strategy",
		DNSStrategy:           string(spec.Strategy),
	}
	m := NewMutator(cfg, slog.Default())
	m.defaultSpec = spec
	return m
}

func mutateAndApply(t *testing.T, m *Mutator, pod *corev1.Pod) *corev1.Pod {
	t.Helper()
	patches, err := m.Mutate(pod)
	require.NoError(t, err)
	if len(patches) == 0 {
		return pod
	}
	return applyPatches(t, pod, patches)
}

func TestMutator_DNS_Options(t *testing.T) {
	t.Run("override replaces whole options array", func(t *testing.T) {
		m := newMutatorWithSpec(t, DNSSpec{
			Options:  []corev1.PodDNSConfigOption{{Name: "ndots", Value: strPtr("2")}},
			Strategy: StrategyOverride,
		})
		pod := &corev1.Pod{Spec: corev1.PodSpec{DNSConfig: &corev1.PodDNSConfig{
			Options: []corev1.PodDNSConfigOption{
				{Name: "ndots", Value: strPtr("9")},
				{Name: "timeout", Value: strPtr("1")},
			},
		}}}
		got := mutateAndApply(t, m, pod)
		require.Len(t, got.Spec.DNSConfig.Options, 1)
		assert.Equal(t, "ndots", got.Spec.DNSConfig.Options[0].Name)
		assert.Equal(t, "2", *got.Spec.DNSConfig.Options[0].Value)
	})

	t.Run("unset removes managed option", func(t *testing.T) {
		m := newMutatorWithSpec(t, DNSSpec{
			Options:  []corev1.PodDNSConfigOption{{Name: "ndots", Value: strPtr("2")}},
			Strategy: StrategyUnset,
		})
		pod := &corev1.Pod{Spec: corev1.PodSpec{DNSConfig: &corev1.PodDNSConfig{
			Options: []corev1.PodDNSConfigOption{
				{Name: "ndots", Value: strPtr("2")},
				{Name: "edns0"},
			},
		}}}
		got := mutateAndApply(t, m, pod)
		require.Len(t, got.Spec.DNSConfig.Options, 1)
		assert.Equal(t, "edns0", got.Spec.DNSConfig.Options[0].Name)
	})

	t.Run("unset removes multiple in descending order", func(t *testing.T) {
		m := newMutatorWithSpec(t, DNSSpec{
			Options: []corev1.PodDNSConfigOption{
				{Name: "ndots", Value: strPtr("2")},
				{Name: "edns0"},
			},
			Strategy: StrategyUnset,
		})
		pod := &corev1.Pod{Spec: corev1.PodSpec{DNSConfig: &corev1.PodDNSConfig{
			Options: []corev1.PodDNSConfigOption{
				{Name: "ndots", Value: strPtr("2")},
				{Name: "timeout", Value: strPtr("1")},
				{Name: "edns0"},
			},
		}}}
		got := mutateAndApply(t, m, pod)
		require.Len(t, got.Spec.DNSConfig.Options, 1)
		assert.Equal(t, "timeout", got.Spec.DNSConfig.Options[0].Name)
	})

	t.Run("update skips absent option", func(t *testing.T) {
		m := newMutatorWithSpec(t, DNSSpec{
			Options:  []corev1.PodDNSConfigOption{{Name: "ndots", Value: strPtr("2")}},
			Strategy: StrategyUpdate,
		})
		pod := &corev1.Pod{Spec: corev1.PodSpec{DNSConfig: &corev1.PodDNSConfig{
			Options: []corev1.PodDNSConfigOption{{Name: "timeout", Value: strPtr("1")}},
		}}}
		patches, err := m.Mutate(pod)
		require.NoError(t, err)
		assert.Empty(t, patches)
	})

	t.Run("update replaces present option", func(t *testing.T) {
		m := newMutatorWithSpec(t, DNSSpec{
			Options:  []corev1.PodDNSConfigOption{{Name: "ndots", Value: strPtr("2")}},
			Strategy: StrategyUpdate,
		})
		pod := &corev1.Pod{Spec: corev1.PodSpec{DNSConfig: &corev1.PodDNSConfig{
			Options: []corev1.PodDNSConfigOption{{Name: "ndots", Value: strPtr("5")}},
		}}}
		got := mutateAndApply(t, m, pod)
		require.Len(t, got.Spec.DNSConfig.Options, 1)
		assert.Equal(t, "2", *got.Spec.DNSConfig.Options[0].Value)
	})
}

func TestMutator_DNS_NameserversAndSearches(t *testing.T) {
	t.Run("merge adds nameservers when absent", func(t *testing.T) {
		m := newMutatorWithSpec(t, DNSSpec{
			Nameservers: []string{"10.0.0.10"},
			Strategy:    StrategyMerge,
		})
		pod := &corev1.Pod{Spec: corev1.PodSpec{DNSConfig: &corev1.PodDNSConfig{}}}
		got := mutateAndApply(t, m, pod)
		assert.Equal(t, []string{"10.0.0.10"}, got.Spec.DNSConfig.Nameservers)
	})

	t.Run("merge unions searches", func(t *testing.T) {
		m := newMutatorWithSpec(t, DNSSpec{
			Searches: []string{"b.local"},
			Strategy: StrategyMerge,
		})
		pod := &corev1.Pod{Spec: corev1.PodSpec{DNSConfig: &corev1.PodDNSConfig{
			Searches: []string{"a.local"},
		}}}
		got := mutateAndApply(t, m, pod)
		assert.Equal(t, []string{"a.local", "b.local"}, got.Spec.DNSConfig.Searches)
	})

	t.Run("override replaces searches", func(t *testing.T) {
		m := newMutatorWithSpec(t, DNSSpec{
			Searches: []string{"only.local"},
			Strategy: StrategyOverride,
		})
		pod := &corev1.Pod{Spec: corev1.PodSpec{DNSConfig: &corev1.PodDNSConfig{
			Searches: []string{"a.local", "b.local"},
		}}}
		got := mutateAndApply(t, m, pod)
		assert.Equal(t, []string{"only.local"}, got.Spec.DNSConfig.Searches)
	})
}

func TestMutator_DNS_NilConfigMultiField(t *testing.T) {
	m := newMutatorWithSpec(t, DNSSpec{
		Nameservers: []string{"10.0.0.10"},
		Options:     []corev1.PodDNSConfigOption{{Name: "ndots", Value: strPtr("2")}},
		Strategy:    StrategyMerge,
	})
	pod := &corev1.Pod{Spec: corev1.PodSpec{}}
	got := mutateAndApply(t, m, pod)
	require.NotNil(t, got.Spec.DNSConfig)
	assert.Equal(t, []string{"10.0.0.10"}, got.Spec.DNSConfig.Nameservers)
	require.Len(t, got.Spec.DNSConfig.Options, 1)
	assert.Equal(t, "ndots", got.Spec.DNSConfig.Options[0].Name)
}

func TestMutator_DNS_Policy(t *testing.T) {
	t.Run("replace dnsPolicy", func(t *testing.T) {
		cf := corev1.DNSClusterFirst
		m := newMutatorWithSpec(t, DNSSpec{
			DNSPolicy: &cf,
			Strategy:  StrategyMerge,
		})
		pod := &corev1.Pod{Spec: corev1.PodSpec{DNSPolicy: corev1.DNSDefault}}
		got := mutateAndApply(t, m, pod)
		assert.Equal(t, corev1.DNSClusterFirst, got.Spec.DNSPolicy)
	})

	t.Run("None without nameservers is skipped (no invalid patch)", func(t *testing.T) {
		none := corev1.DNSNone
		m := newMutatorWithSpec(t, DNSSpec{
			DNSPolicy: &none,
			Strategy:  StrategyMerge,
		})
		pod := &corev1.Pod{Spec: corev1.PodSpec{DNSPolicy: corev1.DNSDefault}}
		got := mutateAndApply(t, m, pod)
		// Policy change skipped because it would produce an invalid pod.
		assert.Equal(t, corev1.DNSDefault, got.Spec.DNSPolicy)
	})

	t.Run("None with nameservers is applied", func(t *testing.T) {
		none := corev1.DNSNone
		m := newMutatorWithSpec(t, DNSSpec{
			DNSPolicy:   &none,
			Nameservers: []string{"1.1.1.1"},
			Strategy:    StrategyMerge,
		})
		pod := &corev1.Pod{Spec: corev1.PodSpec{DNSPolicy: corev1.DNSDefault}}
		got := mutateAndApply(t, m, pod)
		assert.Equal(t, corev1.DNSNone, got.Spec.DNSPolicy)
		assert.Equal(t, []string{"1.1.1.1"}, got.Spec.DNSConfig.Nameservers)
	})
}

func TestMutator_DNS_AnnotationOverlay(t *testing.T) {
	cfg := &config.Config{
		NdotsValue:            2,
		AnnotationMode:        "always",
		SpecAnnotationKey:     "ndots.hawky.dev/dns-config",
		StrategyAnnotationKey: "ndots.hawky.dev/dns-strategy",
		DNSStrategy:           "merge",
	}
	m := NewMutator(cfg, slog.Default())

	t.Run("annotation overlay overrides global ndots", func(t *testing.T) {
		pod := &corev1.Pod{}
		pod.Annotations = map[string]string{
			"ndots.hawky.dev/dns-config": `{"options":[{"name":"ndots","value":"4"}]}`,
		}
		got := mutateAndApply(t, m, pod)
		require.NotNil(t, got.Spec.DNSConfig)
		idx := findOptionIndex(got.Spec.DNSConfig.Options, "ndots")
		require.NotEqual(t, -1, idx)
		assert.Equal(t, "4", *got.Spec.DNSConfig.Options[idx].Value)
	})

	t.Run("strategy annotation overrides global", func(t *testing.T) {
		pod := &corev1.Pod{Spec: corev1.PodSpec{DNSConfig: &corev1.PodDNSConfig{
			Options: []corev1.PodDNSConfigOption{{Name: "ndots", Value: strPtr("2")}},
		}}}
		pod.Annotations = map[string]string{
			"ndots.hawky.dev/dns-strategy": "unset",
		}
		got := mutateAndApply(t, m, pod)
		assert.Equal(t, -1, findOptionIndex(got.Spec.DNSConfig.Options, "ndots"))
	})

	t.Run("malformed annotation falls back to default (fail-open)", func(t *testing.T) {
		pod := &corev1.Pod{}
		pod.Annotations = map[string]string{
			"ndots.hawky.dev/dns-config": `{not valid`,
		}
		patches, err := m.Mutate(pod)
		require.NoError(t, err)
		got := applyPatches(t, pod, patches)
		// Default ndots=2 still applied.
		idx := findOptionIndex(got.Spec.DNSConfig.Options, "ndots")
		require.NotEqual(t, -1, idx)
		assert.Equal(t, "2", *got.Spec.DNSConfig.Options[idx].Value)
	})
}
