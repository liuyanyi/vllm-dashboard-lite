package main

import "strings"

// Return nil if no valid counter interval was observed. Some exporters
// normalize counters with a _total suffix, others keep their original name.
func metricRate(a map[string]*Aggregate, names ...string) *float64 {
	for _, name := range names {
		rate, found := 0.0, false
		for _, g := range a {
			if g.Name == name && g.Kind == "counter" && g.Duration > 0 {
				rate += g.Delta / g.Duration
				found = true
			}
		}
		if found {
			return &rate
		}
	}
	return nil
}

func gaugeSum(s Snapshot, name string) *float64 {
	sum, found := 0.0, false
	for _, v := range s.Values {
		if v.Name == name && v.Kind == "gauge" {
			sum += v.Value
			found = true
		}
	}
	if !found {
		return nil
	}
	return &sum
}

func durationSummary(out map[string]*float64, a map[string]*Aggregate, key, base string) {
	out[key] = ratio(a, base+"_sum", base+"_count")
	out[key+"_p50"] = histogramQuantile(a, base, .5)
	out[key+"_p95"] = histogramQuantile(a, base, .95)
}

func teiSummaries(out map[string]*float64, a map[string]*Aggregate, cur Snapshot) {
	// Do not introduce TEI series into historical vLLM-only data.
	hasTEI := false
	for _, v := range cur.Values {
		if strings.HasPrefix(v.Name, "te_") {
			hasTEI = true
			break
		}
	}
	if !hasTEI {
		for _, g := range a {
			if strings.HasPrefix(g.Name, "te_") {
				hasTEI = true
				break
			}
		}
	}
	if !hasTEI {
		return
	}
	for _, name := range []string{"te_request_count", "te_request_success", "te_request_failure", "te_embed_count", "te_embed_success", "te_predict_count", "te_predict_success"} {
		out[name+"_rate"] = metricRate(a, name, name+"_total")
	}
	out["te_input_tokens_rate"] = metricRate(a, "te_request_input_length_sum")
	out["te_queue_size"] = gaugeSum(cur, "te_queue_size")
	// Polls include empty batches. These are per-poll means, not per-nonempty batch.
	out["te_batch_size_mean"] = ratio(a, "te_batch_next_size_sum", "te_batch_next_size_count")
	out["te_batch_tokens_mean"] = ratio(a, "te_batch_next_tokens_sum", "te_batch_next_tokens_count")
	for _, prefix := range []string{"te_request", "te_embed", "te_predict"} {
		for _, suffix := range []string{"duration", "tokenization_duration", "queue_duration", "inference_duration"} {
			name := prefix + "_" + suffix
			durationSummary(out, a, name+"_seconds", name)
		}
	}
}

// HTTP requests are distinct from engine requests (one rerank request may
// fan out into multiple engine items). Only known inference routes count.
func poolingHTTPSummaries(out map[string]*float64, a map[string]*Aggregate) {
	filtered := map[string]*Aggregate{}
	for k, g := range a {
		if g.Labels["method"] != "POST" {
			continue
		}
		switch g.Labels["handler"] {
		case "/v1/embeddings", "/embeddings", "/v2/embed", "/embed", "/v1/rerank", "/v2/rerank", "/rerank", "/score", "/v1/score", "/pooling", "/v1/pooling":
			filtered[k] = g
		}
	}
	if len(filtered) == 0 {
		return
	}
	out["pooling_api_request_rate"] = metricRate(filtered, "http_requests_total", "http_requests")
	durationSummary(out, filtered, "pooling_api_latency_seconds", "http_request_duration_seconds")
}
