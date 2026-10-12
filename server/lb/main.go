package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"time"
	"os"
	"context"
	"os/signal"
	"syscall"
	"github.com/joho/godotenv"
)

type Backend struct {
	URL     *url.URL
	Proxy   *httputil.ReverseProxy
	Healthy atomic.Bool
}

type BackendStatus struct { 
	URL string `json:"url"` 
	Healthy bool `json:"healthy"` 
}

var backendURLs = []string{
	"http://localhost:8080",
	"http://localhost:8081",
}

var backends []*Backend
var counter uint64

func healthCheck(b *Backend) {
	start := time.Now()
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(b.URL.String() + "/health")
	elapsed := time.Since(start)

	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("health check FAILED for %s after %v: err=%v", b.URL, elapsed, err)
		b.Healthy.Store(false)
		return
	}
	resp.Body.Close()
	log.Printf("health check OK for %s in %v", b.URL, elapsed)
	b.Healthy.Store(true)
}

func startHealthChecks(interval time.Duration) {
	for _, b := range backends {
		healthCheck(b) // initial sync check before serving traffic
	}
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			for _, b := range backends {
				go healthCheck(b)
			}
		}
	}()
}

func lbStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-API-Key") != os.Getenv("LB_STATUS_KEY") {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
	w.Header().Set("Content-Type", "application/json")

	statuses := make([]BackendStatus, 0, len(backends))
	for _, b := range backends {
		statuses = append(statuses, BackendStatus{
			URL:     b.URL.String(),
			Healthy: b.Healthy.Load(),
		})
	}
	json.NewEncoder(w).Encode(statuses)
}

func nextBackend() *Backend {
	n := len(backends)
	start := atomic.AddUint64(&counter, 1)
	for i := 0; i < n; i++ {
		idx := (start + uint64(i)) % uint64(n)
		if backends[idx].Healthy.Load() {
			return backends[idx]
		}
	}
	return nil
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	b := nextBackend()
	if b == nil {
		http.Error(w, "all backend instances unavailable", http.StatusServiceUnavailable)
		return
	}
	log.Printf("routing %s -> %s", r.URL.Path, b.URL)
	b.Proxy.ServeHTTP(w, r)
}

func main() {
	godotenv.Load("../.env")

	for _, raw := range backendURLs {
		target, err := url.Parse(raw)
		if err != nil {
			log.Fatal(err)
		}
		backends = append(backends, &Backend{
			URL:   target,
			Proxy: httputil.NewSingleHostReverseProxy(target),
		})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /lb-status", lbStatusHandler)
	mux.HandleFunc("/", proxyHandler)

	startHealthChecks(10 * time.Second)

	srv := &http.Server{
		Addr:    ":9000",
		Handler: mux,
	}

	go func() {
		log.Println("Load balancer running on http://localhost:9000")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutdown signal received, draining in-flight proxied requests...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("forced shutdown:", err)
	}
	log.Println("load balancer exited cleanly")
}