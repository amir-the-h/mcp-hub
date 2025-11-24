package registry

import (
	"encoding/json"
	"sync"
)

// Tool represents a tool exposed by a plugin
type Tool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	PluginID    string `json:"plugin_id"`
}

// Registry stores registered tools and allows subscriptions for changes
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
	subs  map[chan []Tool]struct{}
}

func New() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
		subs:  make(map[chan []Tool]struct{}),
	}
}

func (r *Registry) RegisterTools(pluginID string, tools []Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range tools {
		t.PluginID = pluginID
		r.tools[t.ID] = t
	}
	r.broadcastLocked()
}

func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

func (r *Registry) Subscribe() chan []Tool {
	ch := make(chan []Tool, 1)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subs[ch] = struct{}{}
	// send initial snapshot
	ch <- r.sliceLocked()
	return ch
}

func (r *Registry) Unsubscribe(ch chan []Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.subs, ch)
	close(ch)
}

func (r *Registry) sliceLocked() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

func (r *Registry) broadcastLocked() {
	snapshot := r.sliceLocked()
	for ch := range r.subs {
		// best effort non-blocking
		select {
		case ch <- snapshot:
		default:
		}
	}
}

// MarshalJSON returns JSON representation of tools
func (r *Registry) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.List())
}

// UnregisterTools removes all tools associated with a plugin
func (r *Registry) UnregisterTools(pluginID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, tool := range r.tools {
		if tool.PluginID == pluginID {
			delete(r.tools, id)
		}
	}
	r.broadcastLocked()
}
