package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"time"
)

// A session describes an observation period, not a guaranteed process lifetime.
func sessionReason(w *Worker, s Snapshot, fingerprint string) string {
	if w.state.Run == 0 {
		return "first_observed"
	}
	if w.manual {
		return "manual"
	}
	if s.Start != "" && w.state.Start != "" && s.Start != w.state.Start {
		return "process_changed"
	}
	if s.Model != "" && w.metricModel != "" && s.Model != w.metricModel {
		return "model_changed"
	}
	if fingerprint != "" && w.fingerprint != "" && fingerprint != w.fingerprint {
		return "model_changed"
	}
	if !w.offlineSince.IsZero() && s.At.Sub(w.offlineSince) >= 30*time.Second {
		return "reconnected"
	}
	return ""
}

func (a *App) modelInfo(ctx context.Context, c Config, preferred string) (string, string, string) {
	res, err := a.get(ctx, c, "/v1/models")
	if err != nil {
		return "", "", ""
	}
	defer res.Body.Close()
	var data struct {
		Data []struct {
			ID   string `json:"id"`
			Root string `json:"root"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&data) != nil || len(data.Data) == 0 {
		return "", "", ""
	}
	sort.Slice(data.Data, func(i, j int) bool {
		if data.Data[i].ID == data.Data[j].ID {
			return data.Data[i].Root < data.Data[j].Root
		}
		return data.Data[i].ID < data.Data[j].ID
	})
	chosen := data.Data[0]
	for _, m := range data.Data {
		if m.ID == preferred {
			chosen = m
			break
		}
	}
	if chosen.ID == "" {
		return "", "", ""
	}
	identity, _ := json.Marshal(chosen)
	metadata, _ := json.Marshal(data)
	return chosen.ID, string(identity), string(metadata)
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
