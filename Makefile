.PHONY: build test test-unit test-integration test-e2e lint docker-build kind-create kind-delete kind-load kind-context deploy undeploy build-tools

IMG ?= k8s-dns-admission-controller:latest
KIND_CLUSTER ?= ndots-dev
KIND_CONTEXT ?= kind-$(KIND_CLUSTER)
KIND_NODE_IMAGE ?= kindest/node:v1.32.11
TOOLS_IMG ?= ndots-test-tools
TMP_DIR ?= $(PWD)/tmp
KUBECONFIG ?= $(TMP_DIR)/config

LINT_IMG ?= golangci/golangci-lint:v2.13.2

# Dockerized commands
# We need --network host for kind to access the cluster if needed, and docker socket
DOCKER_RUN = docker run --rm -u $(shell id -u):$(shell id -g) --group-add $(shell stat -c '%g' /var/run/docker.sock) -v $(PWD):/app -w /app -v $(TMP_DIR):/tmp/.kube -e HOME=/tmp -v /var/run/docker.sock:/var/run/docker.sock --network host
TOOLS_CMD = $(DOCKER_RUN) $(TOOLS_IMG)

# Build tools image
build-tools:
	docker build -t $(TOOLS_IMG) -f Dockerfile.tools .

# Build binary
build:
	go build -o bin/k8s-dns-admission-controller ./cmd/k8s-dns-admission-controller

# Run all tests
test:
	go test -v -race -coverprofile=coverage.out ./...

# Run unit tests only
test-unit:
	go test -v -race ./internal/...

# Run integration tests
test-integration:
	go test -v -race -tags integration ./test/integration/...

# Run E2E tests (requires kind cluster with webhook deployed)
test-e2e:
	$(TOOLS_CMD) kubectl config use-context $(KIND_CONTEXT)
	KUBECONFIG=$(KUBECONFIG) go test -v -timeout 5m -tags e2e ./test/e2e/...

# Run linter
lint:
	docker run --rm -v $(PWD):/app -w /app -v $(shell go env GOCACHE):/root/.cache/go-build -v $(shell go env GOMODCACHE):/go/pkg/mod $(LINT_IMG) golangci-lint run -v

# Build Docker image
docker-build:
	docker build -t $(IMG) .

# Generate TLS certs for local dev
certs:
	./scripts/generate-certs.sh

# Create kind cluster
# We need to use the kind binary from the host if possible because running kind inside docker is tricky (dind)
# Assuming kind is installed on host or we use a dind image properly. 
# For now, let's try using the k8s-sigs/kind image which contains the kind binary.
kind-create: build-tools
	mkdir -p $(TMP_DIR)
	$(TOOLS_CMD) kind create cluster --name $(KIND_CLUSTER) --image $(KIND_NODE_IMAGE) --wait 60s
	@echo "Kind cluster '$(KIND_CLUSTER)' created using $(KIND_NODE_IMAGE)"
	@echo "Kubeconfig written to $(KUBECONFIG)"

# Delete kind cluster
kind-delete: build-tools
	mkdir -p $(TMP_DIR)
	$(TOOLS_CMD) kind delete cluster --name $(KIND_CLUSTER)
	rm -rf $(TMP_DIR)

# Switch to kind context
kind-context:
	$(TOOLS_CMD) kubectl config use-context $(KIND_CONTEXT)

# Load image to kind
kind-load: docker-build
	$(TOOLS_CMD) kind load docker-image $(IMG) --name $(KIND_CLUSTER)

# Deploy to kind (requires cert-manager)
deploy: kind-context kind-load
	@echo "Installing cert-manager..."
	$(TOOLS_CMD) kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.4/cert-manager.yaml
	$(TOOLS_CMD) kubectl wait --for=condition=Available deployment -n cert-manager --all --timeout=120s
	@echo "Deploying k8s-dns-admission-controller..."
	$(TOOLS_CMD) helm upgrade --install ndots ./charts/k8s-dns-admission-controller \
		--namespace dns-system \
		--create-namespace \
		--set image.repository=k8s-dns-admission-controller \
		--set image.tag=latest \
		--set logging.level=debug \
		--set 'dns.options[0].name=ndots' \
		--set 'dns.options[0].value=2' \
		--wait

# Undeploy from kind
undeploy: kind-context
	$(TOOLS_CMD) helm uninstall ndots --namespace dns-system || true
	$(TOOLS_CMD) kubectl delete namespace dns-system || true

# Full E2E workflow
e2e: kind-create deploy test-e2e
	@echo "E2E tests complete!"

# Clean up
clean: kind-delete
	rm -rf bin/ coverage.out
