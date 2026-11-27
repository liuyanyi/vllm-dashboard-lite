package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMinimalYAML(t *testing.T) {
	cs, err := loadStaticConfig(strings.NewReader("endpoints:\n  - id: a\n    url: http://localhost:8000\n"))
	if err != nil || len(cs) != 1 || cs[0].Model != "" || cs[0].Backend != "" || cs[0].Task != "" {
		t.Fatal(cs, err)
	}
}
func TestTEIMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/info" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"model_id":"org/model","served_model_name":"alias","model_type":{"reranker":{}},"model_dtype":"float16","max_batch_tokens":4096,"auto_truncate":false,"version":"1.10.0","secret":"never expose"}`)
	}))
	defer server.Close()
	a := &App{client: server.Client()}
	c := Config{URL: "http://unused", MetadataURL: server.URL}
	m, ok := a.fetchMetadata(context.Background(), c, Metadata{})
	if !ok || m.Backend != "tei" || m.Task != "reranker" || m.Model != "alias" || m.Fields["max_batch_tokens"] != "4096" || m.Fields["auto_truncate"] != "false" || m.Fields["secret"] != "" {
		t.Fatal(m, ok)
	}
	c.Model = "manual"
	effective := resolveMetadata(c, Metadata{}, m)
	if effective.Model != "manual" || effective.Sources["model"] != "yaml" {
		t.Fatal(effective)
	}
}
func TestVLLMMultipleNamesAndTask(t *testing.T) {
	s, err := parseMetrics(strings.NewReader("# TYPE vllm:prompt_tokens_total counter\nvllm:prompt_tokens_total{model_name=\"a\"} 10\nvllm:prompt_tokens_total{model_name=\"b\"} 10\n# TYPE http_requests_total counter\nhttp_requests_total{method=\"POST\",handler=\"/v1/rerank\",status=\"2xx\"} 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := metadataFromMetrics(s)
	if m.Model != "" || len(m.Models) != 2 || m.Backend != "vllm" || m.Task != "reranker" {
		t.Fatal(m)
	}
}
func TestMetadataChangeDoesNotSplitSession(t *testing.T) {
	model := "first"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			fmt.Fprint(w, "# TYPE te_queue_size gauge\nte_queue_size 0\n")
		case "/info":
			fmt.Fprintf(w, `{"model_id":%q,"model_type":{"embedding":{}}}`, model)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	db, err := openDB(filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := &App{db: db, client: server.Client()}
	w := &Worker{cfg: Config{ID:"a",URL:server.URL,MetricsPath:"/metrics",Interval:1000},agg:map[string]*Aggregate{}}
	a.collect(context.Background(), w)
	run := w.state.Run
	if run == 0 || w.state.Model != "first" || w.state.Metadata.Task != "embedding" {
		t.Fatal(w.state)
	}
	model = "second"
	w.nextMetadata = time.Time{}
	a.collect(context.Background(), w)
	if w.state.Run != run || w.state.Model != "second" {
		t.Fatal(w.state)
	}
	var saved string
	if err := db.QueryRow("SELECT model FROM runs WHERE id=?", run).Scan(&saved); err != nil || saved != "first" {
		t.Fatal(saved, err)
	}
}
