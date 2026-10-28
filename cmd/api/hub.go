package main

import (
	"sync"
)

// JobHub manages in-process change notifications for long-polling observers.
type JobHub struct {
	mu          sync.Mutex
	subscribers map[string][]chan struct{}
}

// Global notification hub instance used across the application.
var globalHub = &JobHub{
	subscribers: make(map[string][]chan struct{}),
}

// Subscribe registers a new listener channel for a given job ID.
func (h *JobHub) Subscribe(jobID string) chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan struct{}, 1) // buffered to prevent blocking
	h.subscribers[jobID] = append(h.subscribers[jobID], ch)
	return ch
}

// Unsubscribe removes a listener channel when observation exits.
func (h *JobHub) Unsubscribe(jobID string, ch chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()

	listeners := h.subscribers[jobID]
	for i, listener := range listeners {
		if listener == ch {
			// Remove from slice while preserving order
			h.subscribers[jobID] = append(listeners[:i], listeners[i+1:]...)
			close(ch)
			break
		}
	}
	if len(h.subscribers[jobID]) == 0 {
		delete(h.subscribers, jobID)
	}
}

// NotifyAll wakes up *all* waiting observers for a specific job ID after a database commit.
func (h *JobHub) NotifyAll(jobID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, ch := range h.subscribers[jobID] {
		select {
		case ch <- struct{}{}:
		default:
			// Non-blocking if channel already has a pending signal
		}
	}
}