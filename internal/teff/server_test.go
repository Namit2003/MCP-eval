package teff

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWaitHealthy(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer healthy.Close()
	unhealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unhealthy.Close()
	exited := make(chan struct{})
	close(exited)
	ctx := context.Background()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	if err := waitHealthy(ctx, healthy.URL, make(chan struct{}), 5*time.Second); err != nil {
		t.Errorf("healthy server: %v", err)
	}
	if err := waitHealthy(ctx, unhealthy.URL, make(chan struct{}), 300*time.Millisecond); err == nil || !strings.Contains(err.Error(), "not healthy") {
		t.Errorf("unhealthy server: got %v", err)
	}
	if err := waitHealthy(ctx, unhealthy.URL, exited, 5*time.Second); err == nil || !strings.Contains(err.Error(), "exited") {
		t.Errorf("exited process: got %v", err)
	}
	if err := waitHealthy(cancelled, unhealthy.URL, make(chan struct{}), 5*time.Second); err != context.Canceled {
		t.Errorf("cancelled context: got %v", err)
	}
}

func TestStartPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	_, err = Start(context.Background(), t.TempDir(), port, filepath.Join(t.TempDir(), "mcp-tef.log"))
	if err == nil || !strings.Contains(err.Error(), "is not free") {
		t.Fatalf("got %v", err)
	}
}
