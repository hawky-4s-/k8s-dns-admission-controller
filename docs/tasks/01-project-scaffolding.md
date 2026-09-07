# Task 1.1: Project Scaffolding

**Phase**: 1 - Core Foundation  
**Estimate**: 1-2 hours  
**Dependencies**: None

## Objective

Initialize the Go project with proper structure, build tooling, and containerization.

## Deliverables

- [ ] Go module initialized (`go.mod`)
- [ ] Directory structure created
- [ ] Makefile with common targets
- [ ] Dockerfile for multi-stage build
- [ ] `.golangci.yml` linter configuration

## Implementation Details

### Directory Structure

```
.
├── cmd/
│   └── webhook/
│       └── main.go           # Application entrypoint
├── internal/
│   ├── admission/            # Webhook handlers and mutation logic
│   ├── config/               # Configuration management
│   └── server/               # HTTP server setup
├── deploy/
│   └── kubernetes/           # Raw manifests (for development)
├── charts/
│   └── k8s-dns-admission-controller/        # Helm chart
├── scripts/
│   └── generate-certs.sh     # TLS cert generation for dev
├── test/
│   ├── integration/
│   └── e2e/
├── Dockerfile
├── Makefile
├── go.mod
└── .golangci.yml
```

### Makefile Targets

```makefile
.PHONY: build test lint docker-build

# Build binary
build:
	go build -o bin/webhook ./cmd/webhook

# Run tests
test:
	go test -v -race -coverprofile=coverage.out ./...

# Run linter
lint:
	golangci-lint run

# Build Docker image
docker-build:
	docker build -t $(IMG) .

# Generate TLS certs for local dev
certs:
	./scripts/generate-certs.sh

# Create kind cluster
kind-create:
	kind create cluster --name ndots-dev

# Load image to kind
kind-load:
	kind load docker-image $(IMG) --name ndots-dev
```

### Dockerfile

```dockerfile
# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o webhook ./cmd/webhook

# Runtime stage
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /app/webhook .
USER 65532:65532
ENTRYPOINT ["/webhook"]
```

### Go Module

```bash
go mod init github.com/<org>/k8s-dns-admission-controller
```

### Dependencies to Add

```bash
go get k8s.io/api@v0.29.0
go get k8s.io/apimachinery@v0.29.0
go get sigs.k8s.io/controller-runtime@v0.17.0
```

## Acceptance Criteria

- [ ] `make build` produces a working binary
- [ ] `make lint` runs without configuration errors
- [ ] `make docker-build` creates a valid container image
- [ ] Directory structure matches the specification
- [ ] Go module has correct dependencies

## Notes

- Use Go 1.22+ for `slog` support
- Use distroless base image for security
- Set up `.gitignore` properly (already exists)
