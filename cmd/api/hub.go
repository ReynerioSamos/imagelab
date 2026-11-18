package main

import (
	"sync"
	"sync/atomic"
)

// JobHub manages in-process change notifications for long-polling observers.
type JobHub struct {
	mu          sync.Mutex
	subscribers map[string][]chan struct{}
	active int64 // atomic counter of active observers
	peak int64 // atomic peak of active observers
}

// Global notification hub instance used across the application.
var globalHub = &JobHub{
	subscribers: make(map[string][]chan struct{}),
}

// Subscribe registers a new listener channel for a given job ID.
func (h *JobHub) Subscribe(jobID string) chan struct{} {
	h.mu.Lock()
	ch := make(chan struct{}, 1)
	h.subscribers[jobID] = append(h.subscribers[jobID], ch)
	h.mu.Unlock()

	cur := atomic.AddInt64(&h.active, 1)
	for {
		p := atomic.LoadInt64(&h.peak)
		if cur <= p || atomic.CompareAndSwapInt64(&h.peak, p, cur) {
			break
		}
	}
	return ch
}


// Unsubscribe removes a listener channel when observation exits.
func (h *JobHub) Unsubscribe(jobID string, ch chan struct{}) {
	h.mu.Lock()
	ls := h.subscribers[jobID]
	for i, l := range ls {
		if l == ch {
			h.subscribers[jobID] = append(ls[:i], ls[i+1:]...)
			break
		}
	}
	if len(h.subscribers[jobID]) == 0 {
		delete(h.subscribers, jobID)
	}
	h.mu.Unlock()
	atomic.AddInt64(&h.active, -1)
	close(ch)
}

// NotifyAll wakes up *all* waiting observers for a specific job ID after a database commit.
func (h *JobHub) NotifyAll(jobID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subscribers[jobID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (h *JobHub) Stats() (int64, int64) {
	return atomic.LoadInt64(&h.active), atomic.LoadInt64(&h.peak)
}