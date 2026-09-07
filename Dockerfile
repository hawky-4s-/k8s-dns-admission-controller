# Build stage
FROM golang:1.27.1-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o k8s-dns-admission-controller ./cmd/k8s-dns-admission-controller

# Runtime stage
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /app/k8s-dns-admission-controller .
USER 65532:65532
ENTRYPOINT ["/k8s-dns-admission-controller"]
