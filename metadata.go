package main

import (
	"context"
	"encoding/json"
	"io"
	"sort"
	"strings"
)

// Metadata is descriptive only: it never changes the worker ID or session reason.
// Only an allowlist of serving parameters is exposed; no raw env/config dumps.
type Metadata struct {
	Backend string            `json:"backend,omitempty"`
	Task    string            `json:"task,omitempty"`
	Model   string            `json:"model,omitempty"`
	Models  []string          `json:"models,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
	Sources map[string]string `json:"sources,omitempty"`
}

func unique(values map[string]bool) string {
	if len(values) == 1 {
		for v := range values {
			return v
		}
	}
	return ""
}
func knownModels(values map[string]bool) []string {
	var names []string
	for v := range values {
		names = append(names, v)
	}
	sort.Strings(names)
	return names
}

func metadataFromMetrics(s Snapshot) Metadata {
	backends, tasks, models := map[string]bool{}, map[string]bool{}, map[string]bool{}
	cacheFields := map[string]map[string]bool{}
	for _, v := range s.Values {
		if strings.HasPrefix(v.Name, "vllm:") {
			backends["vllm"] = true
			if name := v.Labels["model_name"]; name != "" {
				models[name] = true
			}
		}
		if strings.HasPrefix(v.Name, "te_") {
			backends["tei"] = true
		}
		if v.Name == "vllm:generation_tokens_total" && v.Value > 0 {
			tasks["generation"] = true
		}
		// Positive successful HTTP counts provide evidence of actual task use.
		// Merely registering a route or a zero counter is not evidence.
		if (v.Name == "http_requests_total" || v.Name == "http_requests") && v.Value > 0 && v.Labels["method"] == "POST" && strings.HasPrefix(v.Labels["status"], "2") {
			switch v.Labels["handler"] {
			case "/v1/chat/completions", "/v1/completions", "/v1/responses":
				tasks["generation"] = true
			case "/v1/embeddings", "/embeddings", "/v2/embed", "/embed":
				tasks["embedding"] = true
			case "/v1/rerank", "/v2/rerank", "/rerank", "/score", "/v1/score":
				tasks["reranker"] = true
			}
		}
		if v.Name == "vllm:cache_config_info" {
			for _, key := range []string{"block_size", "cache_dtype", "cpu_offload_gb", "enable_prefix_caching", "gpu_memory_utilization", "num_gpu_blocks", "num_cpu_blocks"} {
				if value := v.Labels[key]; value != "" {
					if cacheFields[key] == nil {
						cacheFields[key] = map[string]bool{}
					}
					cacheFields[key][value] = true
				}
			}
		}
	}
	out := Metadata{Backend: unique(backends), Model: unique(models), Models: knownModels(models), Fields: map[string]string{}}
	if out.Backend == "vllm" {
		out.Task = unique(tasks)
	}
	for k, values := range cacheFields {
		if value := unique(values); value != "" {
			out.Fields[k] = value
		}
	}
	return out
}

func (a *App) getMetadataJSON(ctx context.Context, c Config, path string, dst any) bool {
	if c.MetadataURL != "" {
		c.URL = c.MetadataURL
	}
	res, err := a.get(ctx, c, path)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(dst) == nil
}

func (a *App) fetchMetadata(ctx context.Context, c Config, metrics Metadata) (Metadata, bool) {
	backend := c.Backend
	if backend == "" {
		backend = metrics.Backend
	}
	if backend == "" || backend == "tei" {
		var info struct {
			ModelID string `json:"model_id"`
			ServedModelName string `json:"served_model_name"`
			ModelType map[string]json.RawMessage `json:"model_type"`
		}
		var raw map[string]json.RawMessage
		if a.getMetadataJSON(ctx, c, "/info", &raw) {
			buf, _ := json.Marshal(raw)
			if json.Unmarshal(buf, &info) == nil && info.ModelID != "" && info.ModelType != nil {
				out := Metadata{Backend: "tei", Model: info.ServedModelName, Fields: map[string]string{}}
				if out.Model == "" {
					out.Model = info.ModelID
				}
				out.Models = []string{out.Model}
				if _, ok := info.ModelType["embedding"]; ok {
					out.Task = "embedding"
				} else if _, ok := info.ModelType["reranker"]; ok {
					out.Task = "reranker"
				}
				for _, key := range []string{"model_id", "served_model_name", "model_dtype", "model_sha", "version", "max_input_length", "max_batch_tokens", "max_batch_requests", "max_concurrent_requests", "max_client_batch_size", "auto_truncate", "tokenization_workers"} {
					if value := scalarField(raw[key]); value != "" {
						out.Fields[key] = value
					}
				}
				return out, true
			}
		}
		if backend == "tei" {
			return Metadata{}, false
		}
	}
	if backend == "" || backend == "vllm" {
		var cards struct {
			Data []struct {
				ID string `json:"id"`
				Root string `json:"root"`
				Owner string `json:"owned_by"`
				MaxLen int `json:"max_model_len"`
			} `json:"data"`
		}
		out := Metadata{Backend: backend, Fields: map[string]string{}}
		ok := a.getMetadataJSON(ctx, c, "/v1/models", &cards)
		if ok {
			names := map[string]bool{}
			for _, card := range cards.Data {
				if card.Owner == "vllm" {
					out.Backend = "vllm"
				}
				if card.ID != "" {
					names[card.ID] = true
				}
			}
			out.Models = knownModels(names)
			switch {
			case c.Model != "" && names[c.Model]:
				out.Model = c.Model
			case metrics.Model != "" && names[metrics.Model]:
				out.Model = metrics.Model
			default:
				out.Model = unique(names)
			}
			for _, card := range cards.Data {
				if out.Model != "" && card.ID == out.Model {
					if card.Root != "" {
						out.Fields["model_root"] = card.Root
					}
					if card.MaxLen > 0 {
						b, _ := json.Marshal(card.MaxLen)
						out.Fields["max_model_len"] = string(b)
					}
					break
				}
			}
		}
		if out.Backend == "vllm" {
			var version struct { Version string `json:"version"` }
			if a.getMetadataJSON(ctx, c, "/version", &version) && version.Version != "" {
				out.Fields["version"] = version.Version
				return out, ok || backend == ""
			}
		}
		return out, ok
	}
	return Metadata{}, false
}

func scalarField(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var v any
	if json.Unmarshal(raw, &v) == nil {
		switch v.(type) {
		case bool, float64:
			return string(raw)
		}
	}
	return ""
}

func resolveMetadata(c Config, metrics, remote Metadata) Metadata {
	out := Metadata{Fields: map[string]string{}, Sources: map[string]string{}}
	for key, value := range metrics.Fields {
		out.Fields[key] = value
	}
	for key, value := range remote.Fields {
		out.Fields[key] = value
	}
	choose := func(key, explicit, api, metric string) string {
		switch {
		case explicit != "":
			out.Sources[key] = "yaml"
			return explicit
		case api != "":
			out.Sources[key] = "api"
			return api
		case metric != "":
			out.Sources[key] = "metrics"
			return metric
		default:
			return ""
		}
	}
	out.Backend = choose("backend", c.Backend, remote.Backend, metrics.Backend)
	out.Task = choose("task", c.Task, remote.Task, metrics.Task)
	// Prefer a unique observed model if several model aliases are available;
	// never choose an arbitrary first entry from a list.
	out.Model = choose("model", c.Model, remote.Model, metrics.Model)
	names := map[string]bool{}
	for _, list := range [][]string{remote.Models, metrics.Models} {
		for _, name := range list {
			names[name] = true
		}
	}
	out.Models = knownModels(names)
	return out
}
