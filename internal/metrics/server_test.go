package metrics

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	reg := prometheus.NewRegistry()
	srv := NewServer(reg, 0) // port 0 for random assignment

	require.NotNil(t, srv)
}

func TestServer_StartStop(t *testing.T) {
	reg := prometheus.NewRegistry()
	// Use port 0 to get a random available port
	srv := NewServer(reg, 0)

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := srv.Shutdown(ctx)
	require.NoError(t, err)

	// Check that Start returned without error (or ErrServerClosed)
	startErr := <-errCh
	assert.True(t, startErr == nil || startErr == http.ErrServerClosed)
}

func TestServer_ServesMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	recorder := NewRecorder(reg)

	// Record some metrics
	recorder.RecordMutation("default", "mutated")
	recorder.RecordError("decode")

	// Use port 0 and parse the actual address from the listener
	port := 18081 // Use a high port to avoid conflicts
	srv := NewServer(reg, port)

	// Start server
	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			t.Logf("server error: %v", err)
		}
	}()

	// Give server time to start
	time.Sleep(200 * time.Millisecond)

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	// Make request to /metrics
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", port))
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Read full body
	body := make([]byte, 0)
	buf := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buf)
		body = append(body, buf[:n]...)
		if err != nil {
			break
		}
	}
	bodyStr := string(body)

	// Verify metrics are present
	assert.Contains(t, bodyStr, "dns_webhook_mutations_total")
	assert.Contains(t, bodyStr, "dns_webhook_errors_total")
}
