package main

import (
	"net/http"
	"time"
)

// A session describes an observation period, not a guaranteed process lifetime.
func sessionReason(w *Worker, s Snapshot) string {
	if w.state.Run == 0 {
		return "first_observed"
	}
	if w.manual {
		return "manual"
	}
	if s.Start != "" && w.state.Start != "" && s.Start != w.state.Start {
		return "process_changed"
	}
	if !w.offlineSince.IsZero() && s.At.Sub(w.offlineSince) >= 30*time.Second {
		return "reconnected"
	}
	return ""
}

func (a *App) newSession(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	x := a.workers[r.PathValue("id")]
	a.mu.Unlock()
	if x == nil {
		http.NotFound(w, r)
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	x.manual = true
	// The collector starts the session on the next successful sample.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	jsonOut(w, map[string]bool{"queued": true})
}
