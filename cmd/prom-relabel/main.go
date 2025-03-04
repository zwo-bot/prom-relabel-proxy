package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/zwo-bot/prom-relabel-proxy/internal/config"
	"github.com/zwo-bot/prom-relabel-proxy/internal/proxy"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "configs/config.yaml", "Path to configuration file")
	listenAddr := flag.String("listen", ":8080", "Address to listen on")
	debugMode := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadFromFile(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create proxy
	proxy, err := proxy.New(cfg, *debugMode)
	if err != nil {
		log.Fatalf("Failed to create proxy: %v", err)
	}
	
	if *debugMode {
		log.Printf("Debug logging enabled")
	}

	// Set up HTTP server
	server := &http.Server{
		Addr:    *listenAddr,
		Handler: proxy,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting Prometheus label rewriting proxy on %s", *listenAddr)
		log.Printf("Forwarding requests to %s", cfg.GetTargetPrometheus())
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Set up signal handling for graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Wait for interrupt signal
	<-stop
	log.Println("Shutting down server...")
}
