//go:build integration

package integration

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/hawky-4s-/k8s-ndots-admission-controller/internal/admission"
	"github.com/hawky-4s-/k8s-ndots-admission-controller/internal/config"
	"github.com/hawky-4s-/k8s-ndots-admission-controller/internal/logging"
	"github.com/hawky-4s-/k8s-ndots-admission-controller/internal/metrics"
)

// TestIntegration_FullAdmissionFlow tests the complete admission flow
// from HTTP request to mutation response
//
// Scenario:
//
//	┌──────────────────────┐     ┌──────────┐     ┌────────────────────────┐
//	│ CASE A: plain pod    │────>│          │────>│ ✅ MUTATED: ndots=2    │
//	│ (no dnsConfig)       │     │          │     └────────────────────────┘
//	└──────────────────────┘     │          │
//	                             │ Webhook  │
//	┌──────────────────────┐     │ Handler  │     ┌──────────────-─────────┐
//	│ CASE B: pod with     │────>│          │────>│ 🚫 SKIPPED: opt-out    │
//	│ change-ndots: "false"│     │ (httptest│     └──────────────-─────────┘
//	└──────────────────────┘     │  server) │
//	                             │          │
//	┌──────────────────────┐     │          │     ┌────────────────────────┐
//	│ CASE C: pod already  │────>│          │────>│ 🚫 NO CHANGE: already  │
//	│ has ndots=2          │     │          │     │    has correct value   │
//	└──────────────────────┘     └──────────┘     └────────────────────────┘
func TestIntegration_FullAdmissionFlow(t *testing.T) {
	// Create a full stack with real components
	cfg := &config.Config{
		DNSOptions:             []config.DNSOption{{Name: "ndots", Value: "2"}},
		DNSEnableAnnotationKey: "dns.hawky.dev/dns",
		DNSAnnotationMode:      "opt-out",
	}

	logger := logging.NewLogger("info", "json", os.Stdout)
	reg := prometheus.NewRegistry()
	metricsRecorder := metrics.NewRecorder(reg)

	mutator := admission.NewMutator(cfg, logger)
	handler := admission.NewHandlerWithMetrics(mutator, logger, metricsRecorder)

	// Create test server
	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", handler.HandleMutate)
	server := httptest.NewServer(mux)
	defer server.Close()

	tests := []struct {
		name        string
		pod         corev1.Pod
		namespace   string
		wantMutated bool
		wantNdots   string
	}{
		{
			name: "pod without dnsConfig gets ndots added",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{},
			},
			namespace:   "default",
			wantMutated: true,
			wantNdots:   "2",
		},
		{
			name: "pod with opt-out annotation is skipped",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-skip",
					Namespace: "default",
					Annotations: map[string]string{
						"dns.hawky.dev/dns": "false",
					},
				},
				Spec: corev1.PodSpec{},
			},
			namespace:   "default",
			wantMutated: false,
		},
		{
			name: "pod with correct ndots is not mutated",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-correct",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					DNSConfig: &corev1.PodDNSConfig{
						Options: []corev1.PodDNSConfigOption{
							{Name: "ndots", Value: stringPtr("2")},
						},
					},
				},
			},
			namespace:   "default",
			wantMutated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create admission review
			podBytes, err := json.Marshal(tt.pod)
			require.NoError(t, err)

			review := admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "admission.k8s.io/v1",
					Kind:       "AdmissionReview",
				},
				Request: &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Namespace: tt.namespace,
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
					Object: runtime.RawExtension{
						Raw: podBytes,
					},
				},
			}

			reviewBytes, err := json.Marshal(review)
			require.NoError(t, err)

			// Send request
			resp, err := http.Post(server.URL+"/mutate", "application/json", bytes.NewReader(reviewBytes))
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Parse response
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			var responseReview admissionv1.AdmissionReview
			err = json.Unmarshal(body, &responseReview)
			require.NoError(t, err)

			assert.True(t, responseReview.Response.Allowed)

			if tt.wantMutated {
				assert.NotEmpty(t, responseReview.Response.Patch)
				assert.NotNil(t, responseReview.Response.PatchType)
			} else {
				assert.Empty(t, responseReview.Response.Patch)
			}
		})
	}
}

// TestIntegration_ConcurrentAdmissions tests handling of concurrent requests
//
// Scenario:
//
//	     ┌─── req 1  ──┐
//	     ├─── req 2  ──┤
//	     ├─── req 3  ──┤
//	     ├─── req 4  ──┤
//	     ├─── req 5  ──┤──────>  ┌──────-────┐  ──────>  all 10 return
//	     ├─── req 6  ──┤         │  Webhook  │           HTTP 200
//	     ├─── req 7  ──┤         │  Handler  │           ✅ thread-safe
//	     ├─── req 8  ──┤         └─────-─────┘
//	     ├─── req 9  ──┤
//	     └─── req 10 ──┘
//	      (goroutines)
func TestIntegration_ConcurrentAdmissions(t *testing.T) {
	cfg := &config.Config{
		DNSOptions:             []config.DNSOption{{Name: "ndots", Value: "1"}},
		DNSEnableAnnotationKey: "dns.hawky.dev/dns",
		DNSAnnotationMode:      "always",
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	reg := prometheus.NewRegistry()
	metricsRecorder := metrics.NewRecorder(reg)

	mutator := admission.NewMutator(cfg, logger)
	handler := admission.NewHandlerWithMetrics(mutator, logger, metricsRecorder)

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", handler.HandleMutate)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Send 10 concurrent requests
	numRequests := 10
	done := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "concurrent-pod",
					Namespace: "test-ns",
				},
			}
			podBytes, _ := json.Marshal(pod)
			review := admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Namespace: "test-ns",
					Kind:      metav1.GroupVersionKind{Kind: "Pod"},
					Object:    runtime.RawExtension{Raw: podBytes},
				},
			}
			reviewBytes, _ := json.Marshal(review)

			resp, err := http.Post(server.URL+"/mutate", "application/json", bytes.NewReader(reviewBytes))
			if err != nil {
				t.Logf("Request %d failed: %v", id, err)
				done <- false
				return
			}
			defer resp.Body.Close()

			done <- resp.StatusCode == http.StatusOK
		}(i)
	}

	// Wait for all requests
	successCount := 0
	for i := 0; i < numRequests; i++ {
		if <-done {
			successCount++
		}
	}

	assert.Equal(t, numRequests, successCount, "All concurrent requests should succeed")
}

// TestIntegration_MetricsRecorded tests that metrics are properly recorded
//
// Scenario:
//
//	┌───────-───┐      ┌──────────┐      ┌──────────────────────────────────┐
//	│  Send     │─────>│  Webhook │─────>│  Prometheus Metrics              │
//	│  Request  │      │  Handler │      │                                  │
//	└──────-────┘      └──────────┘      │  ndots_webhook_mutations_total   │
//	                                     │  ✅ counter incremented          │
//	                                     │                                  │
//	                                     │  ndots_webhook_request_          │
//	                                     │  duration_seconds                │
//	                                     │  ✅ histogram recorded           │
//	                                     └──────────────────────────────────┘
func TestIntegration_MetricsRecorded(t *testing.T) {
	cfg := &config.Config{
		DNSOptions:             []config.DNSOption{{Name: "ndots", Value: "2"}},
		DNSEnableAnnotationKey: "dns.hawky.dev/dns",
		DNSAnnotationMode:      "opt-out",
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	reg := prometheus.NewRegistry()
	metricsRecorder := metrics.NewRecorder(reg)

	mutator := admission.NewMutator(cfg, logger)
	handler := admission.NewHandlerWithMetrics(mutator, logger, metricsRecorder)

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", handler.HandleMutate)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Make a request
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
	}
	podBytes, _ := json.Marshal(pod)
	review := admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Namespace: "default",
			Kind:      metav1.GroupVersionKind{Kind: "Pod"},
			Object:    runtime.RawExtension{Raw: podBytes},
		},
	}
	reviewBytes, _ := json.Marshal(review)

	_, err := http.Post(server.URL+"/mutate", "application/json", bytes.NewReader(reviewBytes))
	require.NoError(t, err)

	// Check metrics are recorded
	metricFamilies, err := reg.Gather()
	require.NoError(t, err)

	foundMutations := false
	foundDuration := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "ndots_webhook_mutations_total" {
			foundMutations = true
		}
		if mf.GetName() == "ndots_webhook_request_duration_seconds" {
			foundDuration = true
		}
	}

	assert.True(t, foundMutations, "Mutations metric should be recorded")
	assert.True(t, foundDuration, "Duration metric should be recorded")
}

// TestIntegration_TLSServer tests the server with TLS (when certs are available)
//
// Scenario:
//
//	┌───────-───┐    TLS/HTTPS    ┌──────────────--──────┐      ┌──────────────┐
//	│  Client   │═══════════════> │  httptest.TLSServer  │─────>│  ✅ 200 OK   │
//	│           │  🔒 encrypted   │  (self-signed cert)  │      │  mutation    │
//	└──────-────┘                 └────────────--────────┘      │  works over  │
//	                                                            │  TLS         │
//	                  (skipped in CI)                           └──────────────┘
func TestIntegration_TLSServer(t *testing.T) {
	// Skip if running in CI without certs
	if os.Getenv("CI") != "" {
		t.Skip("Skipping TLS test in CI environment")
	}

	cfg := &config.Config{
		DNSOptions:             []config.DNSOption{{Name: "ndots", Value: "2"}},
		DNSEnableAnnotationKey: "dns.hawky.dev/dns",
		DNSAnnotationMode:      "opt-out",
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	mutator := admission.NewMutator(cfg, logger)
	handler := admission.NewHandler(mutator, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", handler.HandleMutate)

	// Create TLS test server
	server := httptest.NewTLSServer(mux)
	defer server.Close()

	// Create client that accepts self-signed cert
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
	}
	podBytes, _ := json.Marshal(pod)
	review := admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID:       "test-uid",
			Namespace: "default",
			Kind:      metav1.GroupVersionKind{Kind: "Pod"},
			Object:    runtime.RawExtension{Raw: podBytes},
		},
	}
	reviewBytes, _ := json.Marshal(review)

	resp, err := client.Post(server.URL+"/mutate", "application/json", bytes.NewReader(reviewBytes))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestIntegration_WorkloadTypes tests that pods created by different controllers are properly mutated
// This simulates how the Kubernetes API server sends AdmissionReview for pods with ownerReferences
//
// Scenario:
//
//	┌─────────────────────────────────────────────────────────────┐
//	│                    ALL WORKLOAD TYPES                       │
//	│                                                             │
//	│  Deployment ──> ReplicaSet ──> Pod   ✅ ndots=2             │
//	│                                                             │
//	│  StatefulSet ──────────────> Pod     ✅ ndots=2             │
//	│                                                             │
//	│  DaemonSet ────────────────> Pod     ✅ ndots=2             │
//	│                                                             │
//	│  Job ──────────────────────> Pod     ✅ ndots=2             │
//	│                                                             │
//	│  CronJob ──────> Job ──────> Pod     ✅ ndots=2             │
//	│                                                             │
//	│  (webhook intercepts at Pod creation regardless of owner)   │
//	└─────────────────────────────────────────────────────────────┘
func TestIntegration_WorkloadTypes(t *testing.T) {
	cfg := &config.Config{
		DNSOptions:             []config.DNSOption{{Name: "ndots", Value: "2"}},
		DNSEnableAnnotationKey: "dns.hawky.dev/dns",
		DNSAnnotationMode:      "opt-out",
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	reg := prometheus.NewRegistry()
	metricsRecorder := metrics.NewRecorder(reg)

	mutator := admission.NewMutator(cfg, logger)
	handler := admission.NewHandlerWithMetrics(mutator, logger, metricsRecorder)

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", handler.HandleMutate)
	server := httptest.NewServer(mux)
	defer server.Close()

	tests := []struct {
		name            string
		workloadType    string
		ownerKind       string
		ownerAPIVer     string
		generatePodName string
		wantMutated     bool
	}{
		{
			name:            "pod created by Deployment (via ReplicaSet)",
			workloadType:    "Deployment",
			ownerKind:       "ReplicaSet",
			ownerAPIVer:     "apps/v1",
			generatePodName: "my-deployment-abc123-xyz",
			wantMutated:     true,
		},
		{
			name:            "pod created by StatefulSet",
			workloadType:    "StatefulSet",
			ownerKind:       "StatefulSet",
			ownerAPIVer:     "apps/v1",
			generatePodName: "my-statefulset-0",
			wantMutated:     true,
		},
		{
			name:            "pod created by DaemonSet",
			workloadType:    "DaemonSet",
			ownerKind:       "DaemonSet",
			ownerAPIVer:     "apps/v1",
			generatePodName: "my-daemonset-abc123",
			wantMutated:     true,
		},
		{
			name:            "pod created by Job",
			workloadType:    "Job",
			ownerKind:       "Job",
			ownerAPIVer:     "batch/v1",
			generatePodName: "my-job-xyz",
			wantMutated:     true,
		},
		{
			name:            "pod created by CronJob (via Job)",
			workloadType:    "CronJob",
			ownerKind:       "Job",
			ownerAPIVer:     "batch/v1",
			generatePodName: "my-cronjob-12345-xyz",
			wantMutated:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create pod with ownerReference simulating controller-created pod
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.generatePodName,
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: tt.ownerAPIVer,
							Kind:       tt.ownerKind,
							Name:       "my-" + tt.workloadType,
							UID:        "owner-uid-123",
							Controller: boolPtr(true),
						},
					},
					Labels: map[string]string{
						"app": "test-app",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main",
							Image: "nginx:latest",
						},
					},
				},
			}

			podBytes, err := json.Marshal(pod)
			require.NoError(t, err)

			review := admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "admission.k8s.io/v1",
					Kind:       "AdmissionReview",
				},
				Request: &admissionv1.AdmissionRequest{
					UID:       types.UID("test-uid-" + tt.workloadType),
					Namespace: "default",
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
					Object: runtime.RawExtension{
						Raw: podBytes,
					},
				},
			}

			reviewBytes, err := json.Marshal(review)
			require.NoError(t, err)

			resp, err := http.Post(server.URL+"/mutate", "application/json", bytes.NewReader(reviewBytes))
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			var responseReview admissionv1.AdmissionReview
			err = json.Unmarshal(body, &responseReview)
			require.NoError(t, err)

			assert.True(t, responseReview.Response.Allowed, "Pod should be allowed")

			if tt.wantMutated {
				assert.NotEmpty(t, responseReview.Response.Patch, "Pod from %s should be mutated", tt.workloadType)

				// Verify patch contains dnsConfig
				var patchOps []map[string]interface{}
				err = json.Unmarshal(responseReview.Response.Patch, &patchOps)
				require.NoError(t, err)
				assert.NotEmpty(t, patchOps, "Patch should have operations")
			}
		})
	}
}

// TestIntegration_NamespaceMutationAcrossScenarios tests pod mutation across various
// namespace, annotation, and exclusion combinations using the exact bug report config values
//
// Scenario:
//
//	                      ┌──────────────┐
//	                      │   Webhook    │
//	                      │  ndots=2     │
//	                      │  opt-out mode│
//	                      └──────┬───────┘
//	                             │
//	  ┌──────────────────────────┼──────────────────────────────┐
//	  │                          │                              │
//	  ▼                          ▼                              ▼
//	NAMESPACE CHECK          ANNOTATION CHECK            PRIORITY
//
//	default        ✅ mut    no annotation     ✅ mut
//	my-app         ✅ mut    ndots: "true"     ✅ mut   (opt-out ignores "true")
//	kube-system    🚫 excl   ndots: "false"    🚫 skip
//	kube-public    🚫 excl
//	kube-node-lease🚫 excl
//
//	┌─────────────────────────────────────────────────┐
//	│  kube-system + change-ndots: "true"             │
//	│  → 🚫 NOT mutated                               │
//	│  → namespace exclusion WINS over annotation     │
//	└─────────────────────────────────────────────────┘
func TestIntegration_NamespaceMutationAcrossScenarios(t *testing.T) {
	// Full-stack setup with exact bug report config values
	cfg := &config.Config{
		DNSOptions:             []config.DNSOption{{Name: "ndots", Value: "2"}},
		DNSEnableAnnotationKey: "dns.hawky.dev/dns",
		DNSAnnotationMode:      "opt-out",
		NamespaceExclude: []string{"kube-system", "kube-public", "kube-node-lease"},
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	reg := prometheus.NewRegistry()
	metricsRecorder := metrics.NewRecorder(reg)

	mutator := admission.NewMutator(cfg, logger)
	handler := admission.NewHandlerWithMetrics(mutator, logger, metricsRecorder)

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", handler.HandleMutate)
	server := httptest.NewServer(mux)
	defer server.Close()

	tests := []struct {
		name        string
		pod         corev1.Pod
		namespace   string
		wantMutated bool
		wantNdots   string
	}{
		{
			name: "pod in default namespace with no annotation - mutated",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{},
			},
			namespace:   "default",
			wantMutated: true,
			wantNdots:   "2",
		},
		{
			name: "pod in custom my-app namespace - mutated",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "my-app",
				},
				Spec: corev1.PodSpec{},
			},
			namespace:   "my-app",
			wantMutated: true,
			wantNdots:   "2",
		},
		{
			name: "pod in kube-system - NOT mutated",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "kube-system",
				},
				Spec: corev1.PodSpec{},
			},
			namespace:   "kube-system",
			wantMutated: false,
		},
		{
			name: "pod in kube-public - NOT mutated",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "kube-public",
				},
				Spec: corev1.PodSpec{},
			},
			namespace:   "kube-public",
			wantMutated: false,
		},
		{
			name: "pod in kube-node-lease - NOT mutated",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "kube-node-lease",
				},
				Spec: corev1.PodSpec{},
			},
			namespace:   "kube-node-lease",
			wantMutated: false,
		},
		{
			name: "pod with change-ndots false annotation in default - NOT mutated",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"dns.hawky.dev/dns": "false",
					},
				},
				Spec: corev1.PodSpec{},
			},
			namespace:   "default",
			wantMutated: false,
		},
		{
			name: "pod with no annotations in default - mutated",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-no-annotations",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{},
			},
			namespace:   "default",
			wantMutated: true,
			wantNdots:   "2",
		},
		{
			name: "pod with change-ndots true in default - still mutated (opt-out only skips on false)",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"dns.hawky.dev/dns": "true",
					},
				},
				Spec: corev1.PodSpec{},
			},
			namespace:   "default",
			wantMutated: true,
			wantNdots:   "2",
		},
		{
			name: "pod in kube-system with change-ndots true - NOT mutated (namespace exclusion takes priority)",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "kube-system",
					Annotations: map[string]string{
						"dns.hawky.dev/dns": "true",
					},
				},
				Spec: corev1.PodSpec{},
			},
			namespace:   "kube-system",
			wantMutated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create admission review
			podBytes, err := json.Marshal(tt.pod)
			require.NoError(t, err)

			review := admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "admission.k8s.io/v1",
					Kind:       "AdmissionReview",
				},
				Request: &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Namespace: tt.namespace,
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
					Object: runtime.RawExtension{
						Raw: podBytes,
					},
				},
			}

			reviewBytes, err := json.Marshal(review)
			require.NoError(t, err)

			// Send request
			resp, err := http.Post(server.URL+"/mutate", "application/json", bytes.NewReader(reviewBytes))
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Parse response
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			var responseReview admissionv1.AdmissionReview
			err = json.Unmarshal(body, &responseReview)
			require.NoError(t, err)

			assert.True(t, responseReview.Response.Allowed)

			if tt.wantMutated {
				assert.NotEmpty(t, responseReview.Response.Patch, "Expected pod to be mutated")
				assert.NotNil(t, responseReview.Response.PatchType)

				// Parse JSON patch and verify ndots value
				var patchOps []map[string]interface{}
				err = json.Unmarshal(responseReview.Response.Patch, &patchOps)
				require.NoError(t, err)
				assert.NotEmpty(t, patchOps, "Patch should have operations")

				// Find ndots value in the patch
				foundNdots := false
				for _, op := range patchOps {
					value, ok := op["value"]
					if !ok {
						continue
					}
					// The patch value is the dnsConfig object
					if dnsConfig, ok := value.(map[string]interface{}); ok {
						if options, ok := dnsConfig["options"].([]interface{}); ok {
							for _, opt := range options {
								if optMap, ok := opt.(map[string]interface{}); ok {
									if optMap["name"] == "ndots" && optMap["value"] == tt.wantNdots {
										foundNdots = true
									}
								}
							}
						}
					}
				}
				assert.True(t, foundNdots, "Patch should set ndots to %s", tt.wantNdots)
			} else {
				assert.Empty(t, responseReview.Response.Patch, "Expected pod to NOT be mutated")
			}
		})
	}
}

// TestIntegration_HandlerNamespaceResolution tests the interaction between the handler's
// namespace resolution (req.Namespace vs pod.Namespace) and the mutator's namespace filtering.
//
// Scenario:
//
//	In real K8s, pod.metadata.namespace is often EMPTY during admission.
//	The namespace lives in AdmissionRequest.namespace instead.
//
//	┌──────────────────────────┐    ┌────────────────────────────────┐
//	│  AdmissionRequest        │    │  Pod Object                    │
//	│  req.Namespace = ???     │    │  pod.Namespace = ???           │
//	└────────────┬─────────────┘    └───────────────┬────────────────┘
//	             │                                  │
//	             └──────────┬───────────────────────┘
//	                        ▼
//	            ┌───────────────────────┐
//	            │   Handler resolves NS │
//	            │   from req.Namespace  │
//	            └───────────┬───────────┘
//	                        │
//	                        ▼          ⚠️  BUG: resolved NS not
//	              ┌─────────────────┐      passed to mutator!
//	              │     Mutator     │      mutator sees pod.NS = ""
//	              └────────┬────────┘
//	                       │
//	 ┌─────────────────────┼─────────────────────────────┐
//	 │                     │                             │
//	 ▼                     ▼                             ▼
//
//	pod.NS="default"     pod.NS=""                  pod.NS=""
//	req.NS="default"     req.NS="default"           req.NS="kube-system"
//	✅ mutated           ✅ mutated                 🐛 MUTATED (BUG!)
//	(both agree)         (handler resolves ok)       should be excluded
//	                                                 but "" ≠ "kube-system"
//	                                                 so exclude list misses it
//
// BUG CONTEXT: In real Kubernetes admission webhooks, the pod object's Namespace field is
// often empty during admission (Kubernetes sets it on the AdmissionRequest, not on the pod).
// The handler resolves namespace correctly from req.Namespace (handler.go lines 138-141),
// but the resolved value is only used for logging/metrics. The pod struct is passed directly
// to mutator.Mutate() without setting pod.Namespace, so the mutator's namespace filter
// (mutator.go line 31) reads pod.Namespace which may be empty.
//
// When pod.Namespace is empty and the request targets an excluded namespace (e.g. kube-system),
// the empty string won't match the exclude list, causing the mutator to incorrectly mutate
// pods that should be excluded.
func TestIntegration_HandlerNamespaceResolution(t *testing.T) {
	// Full-stack setup with exact bug report config values
	cfg := &config.Config{
		DNSOptions:             []config.DNSOption{{Name: "ndots", Value: "2"}},
		DNSEnableAnnotationKey: "dns.hawky.dev/dns",
		DNSAnnotationMode:      "opt-out",
		NamespaceExclude: []string{"kube-system", "kube-public", "kube-node-lease"},
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	reg := prometheus.NewRegistry()
	metricsRecorder := metrics.NewRecorder(reg)

	mutator := admission.NewMutator(cfg, logger)
	handler := admission.NewHandlerWithMetrics(mutator, logger, metricsRecorder)

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", handler.HandleMutate)
	server := httptest.NewServer(mux)
	defer server.Close()

	tests := []struct {
		name string
		pod  corev1.Pod
		// reqNamespace is the namespace on the AdmissionRequest (set by Kubernetes API server)
		reqNamespace string
		// wantMutated is the EXPECTED correct behavior: should the pod be mutated?
		// NOTE: Some tests may FAIL if the bug is present, which is the intended
		// diagnostic outcome. The wantMutated value reflects the CORRECT behavior
		// (i.e., what SHOULD happen), not necessarily what currently happens.
		wantMutated bool
		wantNdots   string
	}{
		{
			// Baseline: pod.Namespace and req.Namespace both set to 'default'.
			// Both match, so namespace resolution is unambiguous.
			// Expected: mutated (default is not in the exclude list)
			name: "baseline_both_namespaces_match_default_should_mutate",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{},
			},
			reqNamespace: "default",
			wantMutated:  true,
			wantNdots:    "2",
		},
		{
			// Simulates real K8s admission: pod.Namespace is empty, req.Namespace is 'default'.
			// In real K8s, the API server sets namespace on the request, not always on the pod.
			// Expected: mutated (default is not excluded)
			// This tests whether mutation still works when pod.Namespace is empty but
			// the request targets a non-excluded namespace.
			name: "empty_pod_namespace_req_default_should_mutate",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod-empty-ns",
					// Namespace intentionally left empty to simulate real K8s behavior
				},
				Spec: corev1.PodSpec{},
			},
			reqNamespace: "default",
			wantMutated:  true,
			wantNdots:    "2",
		},
		{
			// KEY BUG VECTOR: pod.Namespace is empty, req.Namespace is 'kube-system' (excluded).
			// The handler resolves namespace to 'kube-system' from req.Namespace, but only
			// uses it for logging. The mutator reads pod.Namespace which is "" (empty).
			// Empty string is NOT in the exclude list ["kube-system", "kube-public", "kube-node-lease"],
			// so the namespace filter returns true (should mutate) when it should return false.
			//
			// EXPECTED (correct behavior): NOT mutated (kube-system is excluded)
			// ACTUAL (with bug): mutated (empty string bypasses exclusion check)
			name: "empty_pod_namespace_req_kube_system_should_NOT_mutate_but_bug_may_allow_it",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod-kube-system",
					// Namespace intentionally left empty - this is the bug trigger
				},
				Spec: corev1.PodSpec{},
			},
			reqNamespace: "kube-system",
			wantMutated:  false,
		},
		{
			// Same bug vector as kube-system but for kube-public.
			// pod.Namespace is empty, req.Namespace is 'kube-public' (excluded).
			//
			// EXPECTED (correct behavior): NOT mutated (kube-public is excluded)
			// ACTUAL (with bug): mutated (empty string bypasses exclusion check)
			name: "empty_pod_namespace_req_kube_public_should_NOT_mutate_but_bug_may_allow_it",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod-kube-public",
					// Namespace intentionally left empty - this is the bug trigger
				},
				Spec: corev1.PodSpec{},
			},
			reqNamespace: "kube-public",
			wantMutated:  false,
		},
		{
			// Non-excluded namespace with empty pod.Namespace.
			// pod.Namespace is empty, req.Namespace is 'my-app' (not excluded).
			// Expected: mutated (my-app is not in the exclude list)
			// This should work correctly even with the bug, since 'my-app' is not
			// excluded and empty string is also not excluded.
			name: "empty_pod_namespace_req_my_app_should_mutate",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod-my-app",
					// Namespace intentionally left empty
				},
				Spec: corev1.PodSpec{},
			},
			reqNamespace: "my-app",
			wantMutated:  true,
			wantNdots:    "2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create admission review with potentially different pod.Namespace and req.Namespace
			podBytes, err := json.Marshal(tt.pod)
			require.NoError(t, err)

			review := admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "admission.k8s.io/v1",
					Kind:       "AdmissionReview",
				},
				Request: &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Namespace: tt.reqNamespace,
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
					Object: runtime.RawExtension{
						Raw: podBytes,
					},
				},
			}

			reviewBytes, err := json.Marshal(review)
			require.NoError(t, err)

			// Send request
			resp, err := http.Post(server.URL+"/mutate", "application/json", bytes.NewReader(reviewBytes))
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Parse response
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			var responseReview admissionv1.AdmissionReview
			err = json.Unmarshal(body, &responseReview)
			require.NoError(t, err)

			assert.True(t, responseReview.Response.Allowed)

			if tt.wantMutated {
				assert.NotEmpty(t, responseReview.Response.Patch,
					"Expected pod to be mutated (pod.Namespace=%q, req.Namespace=%q)",
					tt.pod.Namespace, tt.reqNamespace)
				assert.NotNil(t, responseReview.Response.PatchType)

				// Parse JSON patch and verify ndots value
				var patchOps []map[string]interface{}
				err = json.Unmarshal(responseReview.Response.Patch, &patchOps)
				require.NoError(t, err)
				assert.NotEmpty(t, patchOps, "Patch should have operations")

				// Find ndots value in the patch - the patch value is the dnsConfig object
				foundNdots := false
				for _, op := range patchOps {
					value, ok := op["value"]
					if !ok {
						continue
					}
					if dnsConfig, ok := value.(map[string]interface{}); ok {
						if options, ok := dnsConfig["options"].([]interface{}); ok {
							for _, opt := range options {
								if optMap, ok := opt.(map[string]interface{}); ok {
									if optMap["name"] == "ndots" && optMap["value"] == tt.wantNdots {
										foundNdots = true
									}
								}
							}
						}
					}
				}
				assert.True(t, foundNdots, "Patch should set ndots to %s", tt.wantNdots)
			} else {
				assert.Empty(t, responseReview.Response.Patch,
					"Expected pod to NOT be mutated (pod.Namespace=%q, req.Namespace=%q). "+
						"If this fails, it indicates the bug where empty pod.Namespace bypasses "+
						"namespace exclusion for req.Namespace=%q",
					tt.pod.Namespace, tt.reqNamespace, tt.reqNamespace)
			}
		})
	}
}

// TestIntegration_NamespaceExclusion tests that pods in excluded namespaces are not mutated
//
// Scenario:
//
//	Exclude list: [kube-system, kube-public]
//
//	┌─────────-────┐     ┌─────────────┐
//	│  default     │────>│ ✅ MUTATED  │
//	└──────────-───┘     └─────────────┘
//	┌──────────-───┐     ┌─────────────┐
//	│  my-app      │────>│ ✅ MUTATED  │
//	└───────────-──┘     └─────────────┘
//	┌────────────-─┐     ┌─────────────┐
//	│  kube-system │────>│ 🚫 EXCLUDED │
//	└─────────────-┘     └─────────────┘
//	┌────────────-─┐     ┌─────────────┐
//	│  kube-public │────>│ 🚫 EXCLUDED │
//	└───────────-──┘     └─────────────┘
func TestIntegration_NamespaceExclusion(t *testing.T) {
	cfg := &config.Config{
		DNSOptions:             []config.DNSOption{{Name: "ndots", Value: "2"}},
		DNSEnableAnnotationKey: "dns.hawky.dev/dns",
		DNSAnnotationMode:      "opt-out",
		NamespaceExclude: []string{"kube-system", "kube-public"},
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	mutator := admission.NewMutator(cfg, logger)
	handler := admission.NewHandler(mutator, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", handler.HandleMutate)
	server := httptest.NewServer(mux)
	defer server.Close()

	tests := []struct {
		name        string
		namespace   string
		wantMutated bool
	}{
		{
			name:        "default namespace - should mutate",
			namespace:   "default",
			wantMutated: true,
		},
		{
			name:        "kube-system - should NOT mutate",
			namespace:   "kube-system",
			wantMutated: false,
		},
		{
			name:        "kube-public - should NOT mutate",
			namespace:   "kube-public",
			wantMutated: false,
		},
		{
			name:        "custom namespace - should mutate",
			namespace:   "my-app",
			wantMutated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: tt.namespace,
				},
			}
			podBytes, _ := json.Marshal(pod)

			review := admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Namespace: tt.namespace,
					Kind:      metav1.GroupVersionKind{Kind: "Pod"},
					Object:    runtime.RawExtension{Raw: podBytes},
				},
			}
			reviewBytes, _ := json.Marshal(review)

			resp, err := http.Post(server.URL+"/mutate", "application/json", bytes.NewReader(reviewBytes))
			require.NoError(t, err)
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			var responseReview admissionv1.AdmissionReview
			json.Unmarshal(body, &responseReview)

			assert.True(t, responseReview.Response.Allowed)

			if tt.wantMutated {
				assert.NotEmpty(t, responseReview.Response.Patch, "Namespace %s should be mutated", tt.namespace)
			} else {
				assert.Empty(t, responseReview.Response.Patch, "Namespace %s should NOT be mutated", tt.namespace)
			}
		})
	}
}

func stringPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}
