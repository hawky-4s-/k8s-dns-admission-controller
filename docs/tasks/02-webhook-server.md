# Task 1.2: Webhook Server Implementation

**Phase**: 1 - Core Foundation  
**Estimate**: 2-3 hours  
**Dependencies**: Task 1.1

## Objective

Implement the HTTPS server that will serve the admission webhook with proper TLS, health endpoints, and graceful shutdown.

## Deliverables

- [ ] TLS-enabled HTTP server in `internal/server/`
- [ ] Health check endpoints (`/healthz`, `/readyz`)
- [ ] Graceful shutdown handling
- [ ] Main entrypoint in `cmd/webhook/main.go`

## Implementation Details

### Server Package (`internal/server/server.go`)

```go
package server

import (
    "context"
    "crypto/tls"
    "net/http"
    "time"
)

type Config struct {
    Port        int
    TLSCertPath string
    TLSKeyPath  string
    Timeout     time.Duration
}

type Server struct {
    httpServer *http.Server
    config     Config
}

func New(cfg Config, handler http.Handler) (*Server, error) {
    // Load TLS certificates
    // Configure TLS with minimum version 1.2
    // Create http.Server with timeouts
}

func (s *Server) Start() error {
    // Start listening with TLS
}

func (s *Server) Shutdown(ctx context.Context) error {
    // Graceful shutdown
}
```

### Health Endpoints

```go
// GET /healthz - Liveness probe
func (s *Server) HandleHealthz(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("ok"))
}

// GET /readyz - Readiness probe
func (s *Server) HandleReadyz(w http.ResponseWriter, r *http.Request) {
    // Check if server is ready to accept traffic
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("ok"))
}
```

### Main Entrypoint (`cmd/webhook/main.go`)

```go
package main

import (
    "context"
    "os"
    "os/signal"
    "syscall"
    
    "github.com/<org>/k8s-dns-admission-controller/internal/server"
)

func main() {
    // 1. Load configuration
    // 2. Set up logging
    // 3. Create HTTP mux with routes
    // 4. Create and start server
    // 5. Wait for shutdown signal
    // 6. Graceful shutdown with timeout
    
    ctx, stop := signal.NotifyContext(context.Background(), 
        syscall.SIGINT, syscall.SIGTERM)
    defer stop()
    
    // ... server setup ...
    
    <-ctx.Done()
    
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    srv.Shutdown(shutdownCtx)
}
```

### TLS Configuration

```go
tlsConfig := &tls.Config{
    MinVersion: tls.VersionTLS12,
    CipherSuites: []uint16{
        tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
        tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
        tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
        tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
    },
}
```

### HTTP Timeouts

```go
httpServer := &http.Server{
    Addr:              fmt.Sprintf(":%d", cfg.Port),
    Handler:           handler,
    TLSConfig:         tlsConfig,
    ReadTimeout:       10 * time.Second,
    WriteTimeout:      10 * time.Second,
    ReadHeaderTimeout: 5 * time.Second,
    IdleTimeout:       120 * time.Second,
}
```

## Routes

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/mutate` | POST | Admission webhook endpoint |
| `/healthz` | GET | Liveness probe |
| `/readyz` | GET | Readiness probe |
| `/metrics` | GET | Prometheus metrics (Phase 3) |

## Acceptance Criteria

- [ ] Server starts and listens on configured port with TLS
- [ ] `/healthz` returns 200 OK
- [ ] `/readyz` returns 200 OK
- [ ] Server shuts down gracefully on SIGTERM
- [ ] TLS configuration uses secure defaults (TLS 1.2+)
- [ ] All timeouts are properly configured

## Testing

```go
func TestServer_HealthEndpoints(t *testing.T) {
    // Test /healthz returns 200
    // Test /readyz returns 200
}

func TestServer_GracefulShutdown(t *testing.T) {
    // Test server shutdown completes in-flight requests
}
```

## Notes

- Webhook port should default to 8443
- Metrics port (Phase 3) should be separate (8080)
- Consider certificate reload without restart (future enhancement)
