package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/amir-the-h/mcp-hub/internal/config"
	"github.com/amir-the-h/mcp-hub/internal/plugin"
	"github.com/amir-the-h/mcp-hub/internal/registry"
	"github.com/amir-the-h/mcp-hub/internal/server"
	"github.com/amir-the-h/mcp-hub/internal/watcher"
)

func main() {
	configPath := flag.String("config", "config.json", "Path to configuration file")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Initialize registry
	reg := registry.New()

	// Initialize plugin manager
	pm := plugin.NewManager(reg)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("warning: failed to load config from %s: %v", *configPath, err)
		log.Printf("starting with no MCP servers configured")
	} else {
		// Load servers from configuration
		if err := pm.LoadFromConfig(ctx, cfg); err != nil {
			log.Printf("warning: failed to load servers from config: %v", err)
		}
	}

	// Start config watcher
	var configWatcher *watcher.Watcher
	if cfg != nil {
		configWatcher, err = watcher.New(*configPath, pm)
		if err != nil {
			log.Printf("warning: failed to create config watcher: %v", err)
		} else {
			if err := configWatcher.Start(ctx); err != nil {
				log.Printf("warning: failed to start config watcher: %v", err)
			} else {
				defer configWatcher.Stop()
			}
		}
	}

	// Start HTTP server (server.New now returns *http.Server)
	srv := server.New(reg, pm)

	// Allow listen port/address to be overridden via environment variables.
	// Priority: MCP_HUB_PORT, PORT. If value contains a colon assume it's a full
	// address (e.g. "0.0.0.0:8080"); otherwise prepend a colon to treat it as a port.
	if p := os.Getenv("MCP_HUB_PORT"); p != "" {
		if strings.Contains(p, ":") {
			srv.Addr = p
		} else {
			srv.Addr = ":" + p
		}
	} else if p := os.Getenv("PORT"); p != "" {
		if strings.Contains(p, ":") {
			srv.Addr = p
		} else {
			srv.Addr = ":" + p
		}
	}

	go func() {
		log.Printf("mcp-hub listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("server stopped: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	_ = srv.Shutdown(shutdownCtx)

	// Give plugins a moment to exit
	pm.StopAll(shutdownCtx)

	log.Println("shutdown complete")
}
