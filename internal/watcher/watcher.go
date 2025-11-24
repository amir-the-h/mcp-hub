package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/amir-the-h/mcp-hub/internal/config"
	"github.com/fsnotify/fsnotify"
)

// PluginManager interface defines the methods we need from plugin.Manager
type PluginManager interface {
	StartServer(ctx context.Context, name string, cfg config.ServerConfig) error
	StopServer(name string) error
	ReloadServer(ctx context.Context, name string, cfg config.ServerConfig) error
}

// Watcher monitors configuration file for changes
type Watcher struct {
	configPath string
	manager    PluginManager
	watcher    *fsnotify.Watcher
	lastConfig *config.Config
	stopCh     chan struct{}
}

// New creates a new config file watcher
func New(configPath string, manager PluginManager) (*Watcher, error) {
	// Expand ~ to home directory
	if configPath[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		configPath = filepath.Join(home, configPath[1:])
	}

	// Get absolute path
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Load initial config
	initialConfig, err := config.Load(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load initial config: %w", err)
	}

	// Create fsnotify watcher
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	w := &Watcher{
		configPath: absPath,
		manager:    manager,
		watcher:    fsWatcher,
		lastConfig: initialConfig,
		stopCh:     make(chan struct{}),
	}

	return w, nil
}

// Start begins watching the config file
func (w *Watcher) Start(ctx context.Context) error {
	// Watch the config file
	if err := w.watcher.Add(w.configPath); err != nil {
		return fmt.Errorf("failed to watch config file: %w", err)
	}

	log.Printf("watching config file: %s", w.configPath)

	go w.watchLoop(ctx)
	return nil
}

// Stop stops the watcher
func (w *Watcher) Stop() {
	close(w.stopCh)
	w.watcher.Close()
}

// watchLoop is the main event loop
func (w *Watcher) watchLoop(ctx context.Context) {
	// Debounce timer to avoid processing multiple rapid changes
	var debounceTimer *time.Timer
	const debounceDelay = 500 * time.Millisecond

	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// We care about Write and Create events
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				// Reset debounce timer
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(debounceDelay, func() {
					w.handleConfigChange(ctx)
				})
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("watcher error: %v", err)
		}
	}
}

// handleConfigChange processes config file changes
func (w *Watcher) handleConfigChange(ctx context.Context) {
	log.Printf("config file changed, reloading...")

	// Load new config
	newConfig, err := config.Load(w.configPath)
	if err != nil {
		log.Printf("error loading new config: %v", err)
		return
	}

	// Validate new config
	if err := newConfig.Validate(); err != nil {
		log.Printf("invalid config, skipping reload: %v", err)
		return
	}

	// Compare and apply changes
	w.applyConfigChanges(ctx, newConfig)

	// Update last config
	w.lastConfig = newConfig
}

// applyConfigChanges determines what changed and applies updates
func (w *Watcher) applyConfigChanges(ctx context.Context, newConfig *config.Config) {
	oldServers := w.lastConfig.GetEnabledServers()
	newServers := newConfig.GetEnabledServers()

	// Find servers to remove (in old but not in new, or disabled in new)
	for name := range oldServers {
		if _, exists := newServers[name]; !exists {
			log.Printf("removing server: %s", name)
			if err := w.manager.StopServer(name); err != nil {
				log.Printf("error stopping server %s: %v", name, err)
			}
		}
	}

	// Find servers to add or update
	for name, newCfg := range newServers {
		oldCfg, exists := oldServers[name]

		if !exists {
			// New server
			log.Printf("adding server: %s", name)
			if err := w.manager.StartServer(ctx, name, newCfg); err != nil {
				log.Printf("error starting server %s: %v", name, err)
			}
		} else if !configEqual(oldCfg, newCfg) {
			// Server configuration changed
			log.Printf("reloading server: %s", name)
			if err := w.manager.ReloadServer(ctx, name, newCfg); err != nil {
				log.Printf("error reloading server %s: %v", name, err)
			}
		}
	}
}

// configEqual checks if two server configs are equal
func configEqual(a, b config.ServerConfig) bool {
	// Compare JSON representations for deep equality
	aJSON, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bJSON, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return reflect.DeepEqual(aJSON, bJSON)
}
