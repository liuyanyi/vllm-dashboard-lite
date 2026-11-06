package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseIdentity(t *testing.T) {
	s, e := parseMetrics(strings.NewReader("# TYPE process_start_time_seconds gauge\nprocess_start_time_seconds 1.23456789e9\n# TYPE vllm:prompt_tokens_total counter\nvllm:prompt_tokens_total{model_name=\"a\"} 100\n"))
	if e != nil || s.Start != "1234567890.000000000" || s.Model != "a" {
		t.Fatalf("%+v %v", s, e)
	}
}
func TestResetAndGap(t *testing.T) {
	makeS := func(at time.Time, v float64) Snapshot {
		return Snapshot{At: at, Values: map[string]Series{"a": {Name: "a", Kind: "counter", Value: v}}}
	}
	now := time.Now()
	a := map[string]*Aggregate{}
	accumulate(a, makeS(now, 100), makeS(now.Add(time.Second), 110), 4)
	accumulate(a, makeS(now.Add(time.Second), 110), makeS(now.Add(2*time.Second), 2), 4)
	accumulate(a, makeS(now.Add(2*time.Second), 2), makeS(now.Add(10*time.Second), 500), 4)
	if a["a"].Delta != 10 || a["a"].Duration != 1 || a["a"].Resets != 1 {
		t.Fatal(a["a"])
	}
}
func TestHistogramAndWeightedRate(t *testing.T) {
	src := "# TYPE vllm:time_to_first_token_seconds histogram\nvllm:time_to_first_token_seconds_bucket{le=\"1\"} 2\nvllm:time_to_first_token_seconds_bucket{le=\"+Inf\"} 3\nvllm:time_to_first_token_seconds_sum 4\nvllm:time_to_first_token_seconds_count 3\n"
	s, e := parseMetrics(strings.NewReader(src))
	if e != nil || len(s.Values) != 4 {
		t.Fatal(s, e)
	}
}

func TestQuantile(t *testing.T) {
	a := map[string]*Aggregate{"1": {Name: "h_bucket", Labels: map[string]string{"le": "1"}, Delta: 50, Duration: 1}, "2": {Name: "h_bucket", Labels: map[string]string{"le": "2"}, Delta: 100, Duration: 1}, "inf": {Name: "h_bucket", Labels: map[string]string{"le": "+Inf"}, Delta: 100, Duration: 1}}
	v := histogramQuantile(a, "h", .95)
	if v == nil || *v != 1.9 {
		t.Fatal(v)
	}
}

func TestStartAvailability(t *testing.T) {
	cases := []struct{ input, status string }{
		{"# TYPE vllm:num_requests_running gauge\nvllm:num_requests_running 1\n", "absent"},
		{"process_start_time_seconds 123.5\n", "available"},
		{"# TYPE process_start_time_seconds gauge\nprocess_start_time_seconds NaN\n", "invalid"},
		{"# TYPE process_start_time_seconds gauge\nprocess_start_time_seconds{pid=\"1\"} 100\nprocess_start_time_seconds{pid=\"2\"} 200\n", "ambiguous"},
	}
	for _, c := range cases {
		s, e := parseMetrics(strings.NewReader(c.input))
		if e != nil || s.StartStatus != c.status {
			t.Fatalf("status %s expected %s: %v", s.StartStatus, c.status, e)
		}
	}
}
func TestEndpointOrderStable(t *testing.T) {
	a := &App{workers: map[string]*Worker{"b": {cfg: Config{ID: "b", URL: "http://gpu:8001"}}, "a": {cfg: Config{ID: "a", URL: "http://gpu:8000"}}}}
	for i := 0; i < 100; i++ {
		s := a.states(false)
		if s[0].Config.ID != "a" || s[1].Config.ID != "b" {
			t.Fatal(s)
		}
	}
}
