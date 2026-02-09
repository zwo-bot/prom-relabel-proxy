package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zwo-bot/prom-relabel-proxy/internal/config"
	"github.com/zwo-bot/prom-relabel-proxy/internal/proxy"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "configs/config.yaml", "Path to configuration file")
	listenAddr := flag.String("listen", ":8080", "Address to listen on")
	metricsAddr := flag.String("metrics-listen", ":9090", "Address for metrics endpoint (set empty to serve on main port)")
	debugMode := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadFromFile(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create proxy
	p, err := proxy.New(cfg, *debugMode)
	if err != nil {
		log.Fatalf("Failed to create proxy: %v", err)
	}

	if *debugMode {
		log.Printf("Debug logging enabled")
	}

	// Create a context that is cancelled on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Set up main HTTP server with the proxy handler
	mux := http.NewServeMux()
	mux.Handle("/", p)

	var metricsServer *http.Server

	if *metricsAddr == "" || *metricsAddr == *listenAddr {
		// Serve metrics on the same port
		mux.Handle("/metrics", promhttp.Handler())
	} else {
		// Serve metrics on a separate port
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", promhttp.Handler())
		metricsServer = &http.Server{
			Addr:    *metricsAddr,
			Handler: metricsMux,
		}
		go func() {
			log.Printf("Serving metrics on %s/metrics", *metricsAddr)
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Failed to start metrics server: %v", err)
			}
		}()
	}

	server := &http.Server{
		Addr:    *listenAddr,
		Handler: mux,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting Prometheus label rewriting proxy on %s", *listenAddr)
		log.Printf("Forwarding requests to %s", cfg.GetTargetPrometheus())
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for context cancellation (signal)
	<-ctx.Done()
	log.Println("Shutting down server...")

	// Create a deadline for graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown both servers gracefully
	if metricsServer != nil {
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("Metrics server shutdown error: %v", err)
		}
	}

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Server stopped gracefully")
}
