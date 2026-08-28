//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/hawky-4s-/k8s-ndots-admission-controller/internal/admission"
	"github.com/hawky-4s-/k8s-ndots-admission-controller/internal/config"
	"github.com/hawky-4s-/k8s-ndots-admission-controller/internal/logging"
	"github.com/hawky-4s-/k8s-ndots-admission-controller/internal/metrics"
)

// mutateAndApplyPod sends the pod through the real handler stack and applies the
// returned JSON Patch to the pod, returning the mutated pod. This proves the
// emitted patch is a well-formed JSON Patch that applies cleanly — the same
// operation the kube-apiserver performs before it re-validates the pod.
func mutateAndApplyPod(t *testing.T, serverURL string, pod corev1.Pod, namespace string) (corev1.Pod, bool) {
	t.Helper()

	podBytes, err := json.Marshal(pod)
	require.NoError(t, err)

	review := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Request: &admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Namespace: namespace,
			Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
			Object:    runtime.RawExtension{Raw: podBytes},
		},
	}
	reviewBytes, err := json.Marshal(review)
	require.NoError(t, err)

	resp, err := http.Post(serverURL+"/mutate", "application/json", bytes.NewReader(reviewBytes))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var responseReview admissionv1.AdmissionReview
	require.NoError(t, json.Unmarshal(body, &responseReview))
	require.NotNil(t, responseReview.Response)
	require.True(t, responseReview.Response.Allowed, "webhook must always allow (fail-open)")

	if len(responseReview.Response.Patch) == 0 {
		return pod, false
	}

	patch, err := jsonpatch.DecodePatch(responseReview.Response.Patch)
	require.NoError(t, err, "response patch must be a valid JSON Patch")

	patchedBytes, err := patch.Apply(podBytes)
	require.NoError(t, err, "response patch must apply cleanly to the pod")

	var mutated corev1.Pod
	require.NoError(t, json.Unmarshal(patchedBytes, &mutated))
	return mutated, true
}

func newDNSTestServer(t *testing.T, cfg *config.Config) *httptest.Server {
	t.Helper()
	logger := logging.NewLogger("info", "json", os.Stdout)
	reg := prometheus.NewRegistry()
	mutator := admission.NewMutator(cfg, logger)
	handler := admission.NewHandlerWithMetrics(mutator, logger, metrics.NewRecorder(reg))

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", handler.HandleMutate)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func strPtr(s string) *string { return &s }

func findOption(opts []corev1.PodDNSConfigOption, name string) *corev1.PodDNSConfigOption {
	for i := range opts {
		if opts[i].Name == name {
			return &opts[i]
		}
	}
	return nil
}

// TestIntegration_DNSSpecAnnotation proves that a per-pod DNS spec annotation,
// in both JSON and YAML form, overlays the operator default and produces a
// well-formed patch that applies cleanly.
func TestIntegration_DNSSpecAnnotation(t *testing.T) {
	cfg := &config.Config{
		NdotsValue:            2,
		AnnotationMode:        "always",
		DNSStrategy:           "merge",
		SpecAnnotationKey:     "ndots.hawky.dev/dns-config",
		StrategyAnnotationKey: "ndots.hawky.dev/dns-strategy",
	}
	server := newDNSTestServer(t, cfg)

	const jsonSpec = `{"options":[{"name":"ndots","value":"4"}],"searches":["team.svc.cluster.local"]}`
	const yamlSpec = "options:\n  - name: ndots\n    value: \"4\"\nsearches:\n  - team.svc.cluster.local\n"

	for _, tc := range []struct {
		name string
		spec string
	}{
		{"json spec annotation", jsonSpec},
		{"yaml spec annotation", yamlSpec},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "p",
					Annotations: map[string]string{cfg.SpecAnnotationKey: tc.spec},
				},
			}
			mutated, ok := mutateAndApplyPod(t, server.URL, pod, "default")
			require.True(t, ok, "spec annotation should produce a patch")
			require.NotNil(t, mutated.Spec.DNSConfig)

			ndots := findOption(mutated.Spec.DNSConfig.Options, "ndots")
			require.NotNil(t, ndots, "ndots option must be present")
			require.NotNil(t, ndots.Value)
			assert.Equal(t, "4", *ndots.Value, "annotation ndots overrides global default")
			assert.Contains(t, mutated.Spec.DNSConfig.Searches, "team.svc.cluster.local")
		})
	}
}

// TestIntegration_StrategyOverride proves the override strategy (via strategy
// annotation) replaces the whole options array through the real stack.
func TestIntegration_StrategyOverride(t *testing.T) {
	cfg := &config.Config{
		NdotsValue:            2,
		AnnotationMode:        "always",
		DNSStrategy:           "merge",
		SpecAnnotationKey:     "ndots.hawky.dev/dns-config",
		StrategyAnnotationKey: "ndots.hawky.dev/dns-strategy",
	}
	server := newDNSTestServer(t, cfg)

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "p",
			Annotations: map[string]string{
				cfg.StrategyAnnotationKey: "override",
			},
		},
		Spec: corev1.PodSpec{
			DNSConfig: &corev1.PodDNSConfig{
				Options: []corev1.PodDNSConfigOption{
					{Name: "ndots", Value: strPtr("9")},
					{Name: "timeout", Value: strPtr("1")},
				},
			},
		},
	}
	mutated, ok := mutateAndApplyPod(t, server.URL, pod, "default")
	require.True(t, ok)
	require.NotNil(t, mutated.Spec.DNSConfig)
	// override replaces the whole options array with the managed default (ndots=2).
	require.Len(t, mutated.Spec.DNSConfig.Options, 1)
	assert.Equal(t, "ndots", mutated.Spec.DNSConfig.Options[0].Name)
	require.NotNil(t, mutated.Spec.DNSConfig.Options[0].Value)
	assert.Equal(t, "2", *mutated.Spec.DNSConfig.Options[0].Value)
}

// TestIntegration_HelmDrivenDNS proves operator-configured (Helm/env) nameservers
// and searches are applied and merged onto an existing dnsConfig.
func TestIntegration_HelmDrivenDNS(t *testing.T) {
	cfg := &config.Config{
		NdotsValue:            2,
		AnnotationMode:        "always",
		DNSStrategy:           "merge",
		DNSNameservers:        []string{"10.0.0.10"},
		DNSSearches:           []string{"svc.cluster.local"},
		SpecAnnotationKey:     "ndots.hawky.dev/dns-config",
		StrategyAnnotationKey: "ndots.hawky.dev/dns-strategy",
	}
	server := newDNSTestServer(t, cfg)

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p"},
		Spec: corev1.PodSpec{
			DNSConfig: &corev1.PodDNSConfig{
				Searches: []string{"existing.local"},
			},
		},
	}
	mutated, ok := mutateAndApplyPod(t, server.URL, pod, "default")
	require.True(t, ok)
	require.NotNil(t, mutated.Spec.DNSConfig)
	assert.Equal(t, []string{"10.0.0.10"}, mutated.Spec.DNSConfig.Nameservers)
	// merge unions searches: existing preserved, operator search appended.
	assert.Contains(t, mutated.Spec.DNSConfig.Searches, "existing.local")
	assert.Contains(t, mutated.Spec.DNSConfig.Searches, "svc.cluster.local")
	// ndots default still applied.
	assert.NotNil(t, findOption(mutated.Spec.DNSConfig.Options, "ndots"))
}

// TestIntegration_DNSPolicyNoneGuard proves the webhook never emits a
// dnsPolicy=None patch that would leave the pod without nameservers (the API
// server rejects that combination), yet applies None when nameservers exist.
func TestIntegration_DNSPolicyNoneGuard(t *testing.T) {
	t.Run("None without nameservers is skipped", func(t *testing.T) {
		cfg := &config.Config{
			NdotsValue:            2,
			AnnotationMode:        "always",
			DNSStrategy:           "merge",
			DNSPolicy:             "None",
			SpecAnnotationKey:     "ndots.hawky.dev/dns-config",
			StrategyAnnotationKey: "ndots.hawky.dev/dns-strategy",
		}
		server := newDNSTestServer(t, cfg)

		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "p"},
			Spec:       corev1.PodSpec{DNSPolicy: corev1.DNSClusterFirst},
		}
		mutated, _ := mutateAndApplyPod(t, server.URL, pod, "default")
		// Policy change skipped: still ClusterFirst, never an invalid None.
		assert.Equal(t, corev1.DNSClusterFirst, mutated.Spec.DNSPolicy)
	})

	t.Run("None with nameservers is applied", func(t *testing.T) {
		cfg := &config.Config{
			NdotsValue:            2,
			AnnotationMode:        "always",
			DNSStrategy:           "merge",
			DNSPolicy:             "None",
			DNSNameservers:        []string{"1.1.1.1"},
			SpecAnnotationKey:     "ndots.hawky.dev/dns-config",
			StrategyAnnotationKey: "ndots.hawky.dev/dns-strategy",
		}
		server := newDNSTestServer(t, cfg)

		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "p"},
			Spec:       corev1.PodSpec{DNSPolicy: corev1.DNSClusterFirst},
		}
		mutated, ok := mutateAndApplyPod(t, server.URL, pod, "default")
		require.True(t, ok)
		assert.Equal(t, corev1.DNSNone, mutated.Spec.DNSPolicy)
		require.NotNil(t, mutated.Spec.DNSConfig)
		assert.Equal(t, []string{"1.1.1.1"}, mutated.Spec.DNSConfig.Nameservers)
	})
}

// TestIntegration_MalformedSpecAnnotationFailOpen proves a malformed spec
// annotation degrades to the operator default rather than blocking the pod.
func TestIntegration_MalformedSpecAnnotationFailOpen(t *testing.T) {
	cfg := &config.Config{
		NdotsValue:            2,
		AnnotationMode:        "always",
		DNSStrategy:           "merge",
		SpecAnnotationKey:     "ndots.hawky.dev/dns-config",
		StrategyAnnotationKey: "ndots.hawky.dev/dns-strategy",
	}
	server := newDNSTestServer(t, cfg)

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "p",
			Annotations: map[string]string{cfg.SpecAnnotationKey: `{not valid`},
		},
	}
	mutated, ok := mutateAndApplyPod(t, server.URL, pod, "default")
	require.True(t, ok, "should still apply the default spec")
	require.NotNil(t, mutated.Spec.DNSConfig)
	ndots := findOption(mutated.Spec.DNSConfig.Options, "ndots")
	require.NotNil(t, ndots)
	require.NotNil(t, ndots.Value)
	assert.Equal(t, "2", *ndots.Value, "falls back to default ndots=2")
}
