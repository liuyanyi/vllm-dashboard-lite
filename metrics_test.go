package main

import (
	"path/filepath"
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
func TestRunIdentity(t *testing.T) {
	db, e := openDB(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	a, _, _, _ := runIdentity(db, "endpoint", "100.1", "modelA")
	b, m, _, _ := runIdentity(db, "endpoint", "100.1", "modelB")
	c, _, _, _ := runIdentity(db, "endpoint", "200.1", "modelB")
	if a != b || a == c || m != "modelA" {
		t.Fatal(a, b, c, m)
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
