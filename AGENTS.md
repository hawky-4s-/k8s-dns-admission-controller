# AGENTS.md - AI Agent Guidelines

This document provides context and guidelines for AI agents working on the k8s-dns-admission-controller project.

## Project Overview

This is a Kubernetes admission controller that manages the `ndots` DNS configuration option in Pod specifications. It runs as a mutating webhook that intercepts Pod creation/update requests and applies configurable `ndots` settings to the Pod's DNS configuration.

## Technology Stack

- **Language**: Go 1.26+
- **Container Runtime**: containerd / Docker / Podman
- **Orchestration**: Kubernetes
- **Testing**: Go testing, testify, envtest (for integration tests)
- **CI/CD**: GitHub Actions

---

## Project Structure

Follow the [Standard Go Project Layout](https://github.com/golang-standards/project-layout):

```
.
├── cmd/
│   └── webhook/              # Main application entrypoint
│       └── main.go
├── internal/
│   ├── admission/            # Admission webhook logic
│   │   ├── handler.go        # HTTP handlers for webhook
│   │   ├── handler_test.go
│   │   ├── mutator.go        # Pod mutation logic
│   │   └── mutator_test.go
│   ├── config/               # Configuration management
│   │   ├── config.go
│   │   └── config_test.go
│   └── server/               # HTTP/HTTPS server setup
│       ├── server.go
│       └── server_test.go
├── pkg/                      # Public API packages (if any)
├── deploy/
│   ├── kubernetes/           # Kubernetes manifests
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   ├── mutatingwebhookconfiguration.yaml
│   │   └── kustomization.yaml
│   └── helm/                 # Helm chart (optional)
├── scripts/                  # Build and utility scripts
│   ├── generate-certs.sh     # TLS certificate generation
│   └── lint.sh
├── test/
│   ├── e2e/                  # End-to-end tests
│   └── integration/          # Integration tests with envtest
├── Dockerfile
├── Makefile
├── go.mod
├── go.sum
├── README.md
├── AGENTS.md
└── .github/
    └── workflows/
        ├── ci.yaml
        └── release.yaml
```

---

## Go Best Practices

### Code Style

- Follow [Effective Go](https://go.dev/doc/effective_go) guidelines
- Use `gofmt` and `goimports` for formatting
- Run `golangci-lint` with the project's configuration
- Keep functions small and focused (< 50 lines preferred)
- Use meaningful variable names; avoid single letters except for loop indices

### Error Handling

```go
// Always wrap errors with context
if err != nil {
    return fmt.Errorf("failed to decode admission request: %w", err)
}

// Use sentinel errors for expected conditions
var ErrInvalidPod = errors.New("invalid pod specification")
```

### Logging

- Use structured logging with `slog` (Go 1.21+) or `zap`/`logr`
- Include relevant context in log messages (pod name, namespace, etc.)
- Use appropriate log levels: Debug, Info, Warn, Error

```go
slog.Info("mutating pod",
    "namespace", pod.Namespace,
    "name", pod.Name,
    "ndots", ndotsValue,
)
```

### Dependency Injection

- Pass dependencies explicitly; avoid global state
- Use interfaces for testability
- Define interfaces in the consuming package, not the implementing package

### Kubernetes Client Best Practices

- Use `controller-runtime` for client operations when possible
- Always respect rate limiting and use caching
- Handle API errors gracefully (not found, conflict, etc.)

---

## Development Methodology: Test-Driven Development (TDD)

> **MANDATORY**: All code in this project MUST be developed using TDD. Write tests FIRST.

### The TDD Cycle

1. **🔴 Red**: Write a failing test that describes the desired behavior
2. **🟢 Green**: Write the minimum code necessary to make the test pass
3. **🔵 Refactor**: Clean up the code while keeping tests green

### TDD Rules

```
✅ DO:
- Write the test BEFORE the implementation
- Run tests after writing each test (verify it fails)
- Write minimal code to pass the test
- Refactor only when tests are green
- Commit after each green-refactor cycle

❌ DON'T:
- Write implementation code without a failing test
- Write multiple tests before implementing
- Skip the "verify test fails" step
- Refactor while tests are red
```

### Example TDD Flow

```go
// Step 1: Write failing test first
func TestMutator_Mutate_NoDNSConfig(t *testing.T) {
    mutator := NewMutator(&Config{NdotsValue: 2}, slog.Default())
    pod := &corev1.Pod{} // No dnsConfig
    
    patches, err := mutator.Mutate(pod)
    
    require.NoError(t, err)
    require.Len(t, patches, 1)
    assert.Equal(t, "add", patches[0].Op)
    assert.Equal(t, "/spec/dnsConfig", patches[0].Path)
}

// Step 2: Run test → FAILS (Red) ✓
// Step 3: Implement Mutate() method
// Step 4: Run test → PASSES (Green) ✓
// Step 5: Refactor if needed
// Step 6: Commit
```

### Test Organization

```
internal/
├── admission/
│   ├── mutator.go           # Implementation
│   ├── mutator_test.go      # Unit tests (written FIRST)
│   ├── handler.go
│   └── handler_test.go
├── config/
│   ├── config.go
│   └── config_test.go
```

### When to Write Tests

| Scenario | Test Type | When to Write |
|----------|-----------|---------------|
| New function/method | Unit test | BEFORE implementation |
| Bug fix | Regression test | BEFORE fixing the bug |
| New feature | Feature test | BEFORE implementing feature |
| Refactoring | N/A | Tests should already exist |

---

## Testing Guidelines

### Test File Naming

- Unit tests: `*_test.go` in the same package
- Integration tests: `test/integration/`
- E2E tests: `test/e2e/`

### Unit Tests

```bash
# Run all unit tests
go test ./...

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# Run specific test
go test -v -run TestMutatePod ./internal/admission/
```

- Aim for 80%+ coverage on business logic
- Use table-driven tests for multiple scenarios
- Mock external dependencies using interfaces

```go
func TestMutatePod(t *testing.T) {
    tests := []struct {
        name        string
        pod         *corev1.Pod
        wantNdots   string
        wantErr     bool
    }{
        {
            name:      "pod without dnsConfig gets default ndots",
            pod:       &corev1.Pod{},
            wantNdots: "2",
        },
        // ... more test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test implementation
        })
    }
}
```

### Integration Tests

Use `envtest` from controller-runtime for testing against a real API server:

```bash
# Run integration tests (requires envtest binaries)
make test-integration
```

### E2E Tests

```bash
# Run e2e tests against a kind cluster
make test-e2e
```

---

## Development Workflow

### Prerequisites

- Go 1.26.4+
- Docker or Podman
- kubectl
- kind (for local Kubernetes cluster)
- golangci-lint

### Common Commands

```bash
# Build the binary
make build

# Run locally (requires TLS certs)
make run

# Build container image
make docker-build IMG=<registry>/k8s-dns-admission-controller:dev

# Deploy to local kind cluster
make kind-create
make kind-load IMG=<registry>/k8s-dns-admission-controller:dev
make deploy

# Run linting
make lint

# Run all tests
make test

# Generate TLS certificates for local development
make certs
```

### Local Development with Kind

1. Create a kind cluster: `make kind-create`
2. Build and load the image: `make docker-build kind-load`
3. Deploy the webhook: `make deploy`
4. Test with a sample pod: `kubectl apply -f deploy/samples/test-pod.yaml`

### Debugging

- Set `LOG_LEVEL=debug` environment variable for verbose logging
- Use `kubectl logs -f deployment/k8s-dns-admission-controller` to follow logs
- Check webhook configuration: `kubectl get mutatingwebhookconfiguration`

---

## CI/CD Pipeline

### GitHub Actions Workflows

#### CI Workflow (`.github/workflows/ci.yaml`)

Triggered on: push to main, pull requests

Steps:
1. **Lint**: Run golangci-lint
2. **Test**: Run unit tests with coverage
3. **Build**: Ensure the binary compiles
4. **Security**: Run gosec and trivy for vulnerability scanning
5. **Integration**: Run integration tests with envtest

#### Release Workflow (`.github/workflows/release.yaml`)

Triggered on: tag push (v*)

Steps:
1. Run full CI
2. Build multi-arch container images (amd64, arm64)
3. Push to container registry
4. Create GitHub release with changelog
5. Generate Helm chart (if applicable)

### Required Secrets

- `REGISTRY_USERNAME`: Container registry username
- `REGISTRY_PASSWORD`: Container registry password/token

---

## Kubernetes Admission Controller Specifics

### Webhook Configuration

The mutating webhook intercepts Pod creation and updates:

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: k8s-dns-admission-controller
webhooks:
  - name: ndots.admission.k8s.io
    rules:
      - apiGroups: [""]
        apiVersions: ["v1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["pods"]
    clientConfig:
      service:
        name: k8s-dns-admission-controller
        namespace: dns-system
        path: /mutate
    admissionReviewVersions: ["v1"]
    sideEffects: None
    failurePolicy: Fail  # or Ignore for non-critical mutations
```

### TLS Requirements

- Webhooks require TLS; certificates must be valid for the service DNS name
- Options for certificate management:
  - cert-manager with a self-signed issuer
  - Manual certificate generation with `scripts/generate-certs.sh`

### Health Endpoints

- `/healthz` - Liveness probe
- `/readyz` - Readiness probe
- `/metrics` - Prometheus metrics (optional)

---

## Configuration

Configuration is loaded from environment variables:

| Variable             | Default | Description                           |
|---------------------|---------|---------------------------------------|
| `DNS_STRATEGY`      | `merge` | Combine strategy: `merge`, `update`, `unset`, `override` |
| `DNS_ANNOTATION_MODE` | `opt-out` | Gate mode: `always`, `opt-in`, `opt-out` |
| `DNS_ANNOTATION_KEY` | `dns.hawky.dev/dns` | Pod annotation for opt-in/opt-out gating |
| `DNS_OPTIONS`       | (unset) | `dnsConfig.options` as `name=value` CSV (e.g. `ndots=2,edns0`) |
| `DNS_NAMESERVERS`   | (unset) | `dnsConfig.nameservers` as CSV |
| `DNS_SEARCHES`      | (unset) | `dnsConfig.searches` as CSV |
| `DNS_POLICY`        | (unset) | Pod `dnsPolicy` to set |
| `DNS_SPEC_ANNOTATION_KEY` | `dns.hawky.dev/dns-config` | Pod annotation carrying a full DNS spec |
| `DNS_STRATEGY_ANNOTATION_KEY` | `dns.hawky.dev/dns-strategy` | Pod annotation overriding the strategy |
| `TLS_CERT_PATH`     | `/certs/tls.crt` | Path to TLS certificate     |
| `TLS_KEY_PATH`      | `/certs/tls.key` | Path to TLS private key     |
| `LOG_LEVEL`         | `info`  | Logging level (debug, info, warn, error) |
| `PORT`              | `8443`  | Webhook server port                   |
| `METRICS_PORT`      | `8080`  | Metrics server port                   |

---

## Important Files

| File | Purpose |
|------|---------|
| `cmd/webhook/main.go` | Application entrypoint |
| `internal/admission/handler.go` | Webhook HTTP handlers |
| `internal/admission/mutator.go` | Core mutation logic |
| `deploy/kubernetes/` | Kubernetes deployment manifests |
| `Makefile` | Build and development commands |
| `.golangci.yml` | Linter configuration |

---

## Security Considerations

1. **RBAC**: Webhook only needs permissions to read ConfigMaps/Secrets for configuration
2. **Network Policies**: Restrict ingress to API server only
3. **Pod Security**: Run as non-root, read-only filesystem
4. **TLS**: Use TLS 1.2+ with strong cipher suites
5. **Image Scanning**: Use Trivy in CI to scan for vulnerabilities

---

## Contributing Guidelines

1. Fork and create a feature branch
2. Write tests for new functionality
3. Ensure `make lint test` passes
4. Submit a PR with a clear description
5. Address review feedback

### Commit Message Format

Follow [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/#summary):

```
feat: add namespace exclusion configuration
fix: handle nil dnsConfig in pod spec
docs: update deployment instructions
test: add integration tests for webhook handler
```

Common types: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `ci`, `build`, `perf`, `style`.
Use a scope when it clarifies the change, e.g. `chore(chart): ...`, `ci(release): ...`.
Append `!` after the type/scope (or add a `BREAKING CHANGE:` footer) for breaking changes.
