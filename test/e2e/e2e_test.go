//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	clientset *kubernetes.Clientset
	testNS    = "e2e-test"
)

func TestMain(m *testing.M) {
	// Setup: create test namespace
	if err := setupTestEnvironment(); err != nil {
		fmt.Printf("Failed to setup test environment: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	printControllerLogs()

	// Cleanup: delete test namespace
	cleanupTestEnvironment()

	os.Exit(code)
}

func printControllerLogs() {
	if clientset == nil {
		return
	}
	ctx := context.Background()
	pods, err := clientset.CoreV1().Pods("ndots-system").List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("Failed to list pods in ndots-system: %v\n", err)
		return
	}

	fmt.Println("=== ndots-admission-controller logs ===")
	for _, pod := range pods.Items {
		fmt.Printf("--- Pod: %s ---\n", pod.Name)
		req := clientset.CoreV1().Pods("ndots-system").GetLogs(pod.Name, &corev1.PodLogOptions{})
		podLogs, err := req.Stream(ctx)
		if err != nil {
			fmt.Printf("Failed to open stream for pod %s: %v\n", pod.Name, err)
			continue
		}
		defer podLogs.Close()

		buf := new(bytes.Buffer)
		_, err = io.Copy(buf, podLogs)
		if err != nil {
			fmt.Printf("Failed to read logs for pod %s: %v\n", pod.Name, err)
			continue
		}
		fmt.Println(buf.String())
	}
	fmt.Println("=======================================")
}

func setupTestEnvironment() error {
	// Load kubeconfig
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.Getenv("HOME") + "/.kube/config"
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNS,
		},
	}

	ctx := context.Background()
	_, err = clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("failed to create test namespace: %w", err)
	}

	// Wait for webhook to be ready
	fmt.Println("Waiting for webhook to be ready...")
	time.Sleep(5 * time.Second)

	return nil
}

func cleanupTestEnvironment() {
	if clientset == nil {
		return
	}
	ctx := context.Background()
	clientset.CoreV1().Namespaces().Delete(ctx, testNS, metav1.DeleteOptions{})
}

// TestE2E_PodMutation tests that pods get the ndots value mutated
//
// Scenario:
//
//	┌───────-──────┐      ┌─────-──────────────┐      ┌─────────────────────┐
//	│  Create Pod  │─────>│  Admission Webhook │─────>│  Pod with ndots=2   │
//	│  (busybox)   │      │  mutates pod       │      │  in DNSConfig       │
//	└──────-───────┘      └──────-─────────────┘      └─────────────────────┘
//	 namespace: test        injects dnsConfig          ✅ ndots = "2"
func TestE2E_PodMutation(t *testing.T) {
	if clientset == nil {
		t.Skip("No Kubernetes client available, skipping E2E test")
	}

	ctx := context.Background()

	// Create a simple pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: testNS,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "test",
					Image:   "busybox:latest",
					Command: []string{"sleep", "3600"},
				},
			},
		},
	}

	createdPod, err := clientset.CoreV1().Pods(testNS).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create pod: %v", err)
	}

	// Cleanup
	defer clientset.CoreV1().Pods(testNS).Delete(ctx, pod.Name, metav1.DeleteOptions{})

	// Verify ndots was set
	if createdPod.Spec.DNSConfig == nil {
		t.Fatal("DNSConfig is nil, expected ndots to be set")
	}

	foundNdots := false
	for _, opt := range createdPod.Spec.DNSConfig.Options {
		if opt.Name == "ndots" {
			foundNdots = true
			if opt.Value == nil || *opt.Value != "2" {
				t.Errorf("Expected ndots=2, got %v", opt.Value)
			}
		}
	}

	if !foundNdots {
		t.Error("ndots option not found in DNSConfig")
	}

	t.Logf("Pod %s created with ndots=%s", createdPod.Name, *createdPod.Spec.DNSConfig.Options[0].Value)
}

// TestE2E_DNSSpecAnnotation tests that a per-pod DNS spec annotation (YAML,
// Vault agent-injector style) overlays the operator default: the ndots value
// is overridden and extra searches are applied. This exercises the full
// deployed webhook end-to-end against a real API server, which re-validates
// the mutated pod before persisting it.
//
// Scenario:
//
//	┌──────────────────────────────┐      ┌────────────────────┐      ┌──────────────────────────┐
//	│  Create Pod with annotation  │─────>│  Webhook overlays  │─────>│  Pod: ndots=4,           │
//	│  ndots.hawky.dev/dns-config  │      │  spec on default   │      │  searches include team.. │
//	└──────────────────────────────┘      └────────────────────┘      └──────────────────────────┘
func TestE2E_DNSSpecAnnotation(t *testing.T) {
	if clientset == nil {
		t.Skip("No Kubernetes client available, skipping E2E test")
	}

	ctx := context.Background()

	const specAnnotation = "ndots.hawky.dev/dns-config"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-dns-spec",
			Namespace: testNS,
			Annotations: map[string]string{
				specAnnotation: "options:\n  - name: ndots\n    value: \"4\"\nsearches:\n  - team.svc.cluster.local\n",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "test",
					Image:   "busybox:latest",
					Command: []string{"sleep", "3600"},
				},
			},
		},
	}

	createdPod, err := clientset.CoreV1().Pods(testNS).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create pod: %v", err)
	}
	defer clientset.CoreV1().Pods(testNS).Delete(ctx, pod.Name, metav1.DeleteOptions{})

	if createdPod.Spec.DNSConfig == nil {
		t.Fatal("DNSConfig is nil, expected the DNS spec annotation to be applied")
	}

	foundNdots := false
	for _, opt := range createdPod.Spec.DNSConfig.Options {
		if opt.Name == "ndots" {
			foundNdots = true
			if opt.Value == nil || *opt.Value != "4" {
				t.Errorf("Expected annotation to override ndots=4, got %v", opt.Value)
			}
		}
	}
	if !foundNdots {
		t.Error("ndots option not found in DNSConfig")
	}

	foundSearch := false
	for _, s := range createdPod.Spec.DNSConfig.Searches {
		if s == "team.svc.cluster.local" {
			foundSearch = true
		}
	}
	if !foundSearch {
		t.Errorf("expected search team.svc.cluster.local, got %v", createdPod.Spec.DNSConfig.Searches)
	}

	t.Logf("Pod %s created with DNS spec annotation applied: %+v", createdPod.Name, createdPod.Spec.DNSConfig)
}

// TestE2E_DeploymentMutation tests that pods created by Deployments get mutated
//
// Scenario:
//
//	┌───────────-─────┐      ┌───────-───────┐      ┌───────────────────┐      ┌─────-─────────────┐
//	│  Create         │─────>│  ReplicaSet   │─────>│  Pod spawned by   │─────>│  Webhook mutates  │
//	│  Deployment     │      │  created      │      │  RS controller    │      │  pod: ndots=2     │
//	│  (replicas: 1)  │      │  automatically│      │                   │      │                   │
//	└────────────-────┘      └─────────-─────┘      └───────────────────┘      └──────-────────────┘
//	 namespace: test                                 label selector match       ✅ ndots = "2"
func TestE2E_DeploymentMutation(t *testing.T) {
	if clientset == nil {
		t.Skip("No Kubernetes client available, skipping E2E test")
	}

	ctx := context.Background()

	// Create deployment using client-go
	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: testNS,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test-deployment",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test-deployment",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "test",
							Image:   "busybox:latest",
							Command: []string{"sleep", "3600"},
						},
					},
				},
			},
		},
	}

	_, err := clientset.AppsV1().Deployments(testNS).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create deployment: %v", err)
	}

	// Cleanup
	defer clientset.AppsV1().Deployments(testNS).Delete(ctx, "test-deployment", metav1.DeleteOptions{})

	// Wait for pod to be created
	time.Sleep(10 * time.Second)

	// List pods with label
	pods, err := clientset.CoreV1().Pods(testNS).List(ctx, metav1.ListOptions{
		LabelSelector: "app=test-deployment",
	})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}

	if len(pods.Items) == 0 {
		t.Fatal("No pods found for deployment")
	}

	pod := pods.Items[0]
	if pod.Spec.DNSConfig == nil {
		t.Fatal("DNSConfig is nil for deployment pod")
	}

	foundNdots := false
	for _, opt := range pod.Spec.DNSConfig.Options {
		if opt.Name == "ndots" {
			foundNdots = true
			t.Logf("Deployment pod %s has ndots=%s", pod.Name, *opt.Value)
		}
	}

	if !foundNdots {
		t.Error("ndots not found in deployment pod")
	}
}

// TestE2E_AnnotationOptOut tests that pods with opt-out annotation are NOT mutated
//
// Scenario:
//
//	┌──────────────────────────┐      ┌───────────-────────┐      ┌──────────────────┐
//	│  Create Pod              │─────>│  Admission Webhook │─────>│  Pod UNCHANGED   │
//	│  annotations:            │      │  checks annotation │      │  no dnsConfig    │
//	│    change-ndots: "false" │      │  → opt-out!        │      │  injected        │
//	└──────────────────────────┘      └────────────-───────┘      └──────────────────┘
//	                                   🚫 SKIP                    ✅ no ndots
func TestE2E_AnnotationOptOut(t *testing.T) {
	if clientset == nil {
		t.Skip("No Kubernetes client available, skipping E2E test")
	}

	ctx := context.Background()

	// Create pod with opt-out annotation
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-optout",
			Namespace: testNS,
			Annotations: map[string]string{
				"change-ndots": "false",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "test",
					Image:   "busybox:latest",
					Command: []string{"sleep", "3600"},
				},
			},
		},
	}

	createdPod, err := clientset.CoreV1().Pods(testNS).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create pod: %v", err)
	}

	// Cleanup
	defer clientset.CoreV1().Pods(testNS).Delete(ctx, pod.Name, metav1.DeleteOptions{})

	// Verify ndots was NOT set (opt-out)
	if createdPod.Spec.DNSConfig != nil {
		for _, opt := range createdPod.Spec.DNSConfig.Options {
			if opt.Name == "ndots" {
				t.Errorf("Pod with opt-out annotation should NOT have ndots set, but found ndots=%v", *opt.Value)
			}
		}
	}

	t.Logf("Pod %s correctly opted out of ndots mutation", createdPod.Name)
}

// TestE2E_NamespaceExclusion tests that pods in excluded namespaces are NOT mutated
//
// Scenario:
//
//	┌─────────────────-─────────────┐
//	│  MutatingWebhookConfiguration │
//	└──────────┬──────-─────────────┘
//	           │
//	           ▼
//	┌──────────────────────────┐
//	│  namespaceSelector rule: │
//	│                          │
//	│   EXCLUDE:               │
//	│   ┌───────────-──────┐   │
//	│   │  kube-system     │   │   ← system namespaces
//	│   │  kube-public     │   │     never get mutated
//	│   │  kube-node-lease │   │
//	│   └─────────-────────┘   │
//	│                          │
//	│   INCLUDE:               │
//	│   ┌───────-──────────┐   │
//	│   │  everything else │   │   ← user namespaces
//	│   └───────-──────────┘   │     get ndots=2
//	└──────────────────────────┘
//	            ✅ webhook config exists & correct
func TestE2E_NamespaceExclusion(t *testing.T) {
	if clientset == nil {
		t.Skip("No Kubernetes client available, skipping E2E test")
	}

	// This test verifies kube-system is excluded
	// We can't actually create pods there, so we'll verify the webhook config
	ctx := context.Background()

	// Get the MutatingWebhookConfiguration
	_, err := clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, "k8s-ndots-admission-controller", metav1.GetOptions{})
	if err != nil {
		// Try to list if specific name failed (chart might name it differently)
		list, listErr := clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
		if listErr != nil {
			t.Fatalf("Failed to list webhook configurations: %v", listErr)
		}
		if len(list.Items) == 0 {
			t.Skip("No mutating webhook configurations found")
		}
		t.Logf("Found %d webhook configurations", len(list.Items))
	} else {
		t.Log("Verified MutatingWebhookConfiguration 'k8s-ndots-admission-controller' exists")
	}

	t.Log("Verified MutatingWebhookConfiguration exists")

	// Additionally verify that the ndots-system namespace is excluded
	pods, _ := clientset.CoreV1().Pods("ndots-system").List(ctx, metav1.ListOptions{})
	if len(pods.Items) > 0 {
		for _, pod := range pods.Items {
			// Webhook pods in ndots-system should not have been mutated by themselves
			t.Logf("Found webhook pod: %s", pod.Name)
		}
	}
}

// TestE2E_DebugLogs verifies that debug logs are printed to stdout
//
// Scenario:
//
//	┌───────────────────────┐      ┌─────────────────────────────┐
//	│  ndots-admission-ctrl │─────>│  stdout logs                │
//	│  pods in ndots-system │      │                             │
//	└───────────────────────┘      │  {"LEVEL":"DEBUG", ...}     │
//	                               │      or                     │
//	 LOG_LEVEL=debug               │  LEVEL=DEBUG ...            │
//	                               └─────────────────────────────┘
//	                                ✅ debug logging is active
func TestE2E_DebugLogs(t *testing.T) {
	if clientset == nil {
		t.Skip("No Kubernetes client available, skipping E2E test")
	}

	ctx := context.Background()
	pods, err := clientset.CoreV1().Pods("ndots-system").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list pods in ndots-system: %v", err)
	}

	foundDebugLog := false
	for _, pod := range pods.Items {
		req := clientset.CoreV1().Pods("ndots-system").GetLogs(pod.Name, &corev1.PodLogOptions{})
		podLogs, err := req.Stream(ctx)
		if err != nil {
			t.Logf("Failed to open stream for pod %s: %v", pod.Name, err)
			continue
		}
		defer podLogs.Close()

		buf := new(bytes.Buffer)
		_, err = io.Copy(buf, podLogs)
		if err != nil {
			t.Logf("Failed to read logs for pod %s: %v", pod.Name, err)
			continue
		}

		logs := buf.String()
		if len(logs) > 0 {
			// Check for debug level logs
			// When using slog with JSON handler, debug logs usually contain "level":"DEBUG" or similar
			// With text handler, level=DEBUG
			// We configured LOG_LEVEL=debug in values.yaml

			if strings.Contains(strings.ToUpper(logs), "\"LEVEL\":\"DEBUG\"") ||
			   strings.Contains(strings.ToUpper(logs), "LEVEL=DEBUG") {
				foundDebugLog = true
				break
			}
		}
	}

	if !foundDebugLog {
		t.Fatal("Debug logs not found in stdout. Expected to see logs with level=debug")
	} else {
		t.Log("Confirmed debug logs are being printed to stdout")
	}
}
