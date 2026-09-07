#!/bin/bash
set -e

CLUSTER_NAME="ndots-dev"
IMAGE_NAME="k8s-dns-admission-controller:local"

echo "🚀 Starting local development environment setup..."

BIN_DIR="bin"
mkdir -p "${BIN_DIR}"
export PATH="${PWD}/${BIN_DIR}:${PATH}"

# Download kind
if ! command -v kind >/dev/null 2>&1; then
    echo "📥 Downloading kind..."
    curl -Lo "${BIN_DIR}/kind" https://kind.sigs.k8s.io/dl/v0.31.0/kind-linux-amd64
    chmod +x "${BIN_DIR}/kind"
fi

# Download kubectl
if ! command -v kubectl >/dev/null 2>&1; then
    echo "📥 Downloading kubectl..."
    curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
    mv kubectl "${BIN_DIR}/"
    chmod +x "${BIN_DIR}/kubectl"
fi

echo "✅ Tools check passed (kind: $(kind version), kubectl: $(kubectl version --client))"

# 1. Create Kind cluster if not exists
if kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
    echo "✅ Cluster '${CLUSTER_NAME}' already exists"
else
    echo "📦 Creating Kind cluster '${CLUSTER_NAME}'..."
    kind create cluster --name "${CLUSTER_NAME}"
fi

# 2. Build Docker image
echo "🔨 Building Docker image..."
docker build -t "${IMAGE_NAME}" .

# 3. Load image into Kind
echo "kp Loading image into Kind..."
kind load docker-image "${IMAGE_NAME}" --name "${CLUSTER_NAME}"

# 4. Switch context
kubectl cluster-info --context "kind-${CLUSTER_NAME}"

# 5. Generate self-signed certs (using existing script or inline)
echo "🔐 Generating TLS certificates..."
mkdir -p dev-certs
./scripts/generate-certs.sh "k8s-dns-admission-controller.default.svc" "dev-certs"

# 6. Deploy Webhook
echo "🚀 Deploying webhook..."

# Create secret
kubectl create secret tls k8s-dns-admission-controller-tls \
    --cert=dev-certs/tls.crt \
    --key=dev-certs/tls.key \
    --dry-run=client -o yaml | kubectl apply -f -

# Deploy manifests (we need to create them first in Task 4.1/4.2)
# For now, verify what we have locally or create temp manifests
if [ ! -f "deploy/kubernetes/deployment.yaml" ]; then
    echo "⚠️  Deployment manifests not found. Creating temporary manifests..."
    mkdir -p deploy/kubernetes
    
    # Generate CA bundle for webhook config
    CA_BUNDLE=$(cat dev-certs/ca.crt | base64 | tr -d '\n')
    
    cat <<EOF > deploy/kubernetes/manifests.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: k8s-dns-admission-controller
  labels:
    app: k8s-dns-admission-controller
spec:
  replicas: 1
  selector:
    matchLabels:
      app: k8s-dns-admission-controller
  template:
    metadata:
      labels:
        app: k8s-dns-admission-controller
    spec:
      containers:
        - name: webhook
          image: ${IMAGE_NAME}
          imagePullPolicy: Never
          ports:
            - containerPort: 8443
          env:
            - name: TLS_CERT_PATH
              value: "/certs/tls.crt"
            - name: TLS_KEY_PATH
              value: "/certs/tls.key"
            - name: LOG_LEVEL
              value: "debug"
          volumeMounts:
            - name: certs
              mountPath: "/certs"
              readOnly: true
      volumes:
        - name: certs
          secret:
            secretName: k8s-dns-admission-controller-tls
---
apiVersion: v1
kind: Service
metadata:
  name: k8s-dns-admission-controller
spec:
  ports:
    - port: 443
      targetPort: 8443
  selector:
    app: k8s-dns-admission-controller
---
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: k8s-dns-admission-controller
webhooks:
  - name: ndots.admission.k8s.io
    clientConfig:
      service:
        name: k8s-dns-admission-controller
        namespace: default
        path: "/mutate"
      caBundle: ${CA_BUNDLE}
    rules:
      - operations: ["CREATE"]
        apiGroups: [""]
        apiVersions: ["v1"]
        resources: ["pods"]
    admissionReviewVersions: ["v1"]
    sideEffects: None
    timeoutSeconds: 5
    failurePolicy: Fail
EOF
fi

kubectl apply -f deploy/kubernetes/manifests.yaml

# 7. Wait for deployment
echo "⏳ Waiting for webhook to be ready..."
kubectl rollout status deployment/k8s-dns-admission-controller --timeout=60s

# 8. Deploy test pod
echo "🧪 Deploying test pod..."
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
  labels:
    app: test
spec:
  containers:
  - name: nginx
    image: nginx:alpine
  restartPolicy: Never
EOF

echo "⏳ Waiting for test pod..."
sleep 5 # Give admission controller time to work and API to persist
kubectl get pod test-pod -o yaml | grep -A 5 dnsConfig || echo "❌ dnsConfig not found in test pod!"

echo "✅ Setup complete! Check above for dnsConfig output."
