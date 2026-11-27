package main

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestTEIRetrievalMetrics(t *testing.T) {
	scrape := func(n int) Snapshot {
		t.Helper()
		s, err := parseMetrics(strings.NewReader(fmt.Sprintf(`# TYPE te_request_count counter
te_request_count{method="batch"} %d
# TYPE te_embed_success counter
te_embed_success %d
# TYPE te_queue_size gauge
te_queue_size 3
# TYPE te_request_input_length histogram
te_request_input_length_bucket{le="512"} %d
te_request_input_length_bucket{le="+Inf"} %d
te_request_input_length_sum %d
te_request_input_length_count %d
# TYPE te_request_duration histogram
te_request_duration_bucket{le="0.1"} %d
te_request_duration_bucket{le="+Inf"} %d
te_request_duration_sum %g
te_request_duration_count %d
`, n, 4*n, 4*n, 4*n, 100*n, 4*n, n, n, .05*float64(n), n)))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	prev, cur := scrape(2), scrape(4)
	cur.At = prev.At.Add(2*time.Second)
	a := map[string]*Aggregate{}
	accumulate(a, prev, cur, 5)
	out := summaries(a, cur)
	for key, want := range map[string]float64{
		"te_request_count_rate": 1,
		"te_embed_success_rate": 4,
		"te_queue_size": 3,
		"te_input_tokens_rate": 100,
		"te_request_duration_seconds": .05,
		"te_request_duration_seconds_p95": .095,
	} {
		if out[key] == nil || math.Abs(*out[key]-want) > 1e-8 {
			t.Fatalf("%s: got %v want %g", key, out[key], want)
		}
	}
	if out["te_predict_success_rate"] != nil {
		t.Fatal("missing metrics must not become zero")
	}
}

func TestPoolingHTTPRate(t *testing.T) {
	a := map[string]*Aggregate{
		"rerank": {Name: "http_requests_total", Kind: "counter", Labels: map[string]string{"method": "POST", "handler": "/v1/rerank"}, Duration: 2, Delta: 6},
		"metrics": {Name: "http_requests_total", Kind: "counter", Labels: map[string]string{"method": "GET", "handler": "/metrics"}, Duration: 2, Delta: 10},
	}
	out := map[string]*float64{}
	poolingHTTPSummaries(out, a)
	if v := out["pooling_api_request_rate"]; v == nil || *v != 3 {
		t.Fatal(v)
	}
}
