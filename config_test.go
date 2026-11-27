package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

const staticYAML = `endpoints:
  - id: embed-a
    backend: tei
    task: embedding
    model: example/model
    url: http://localhost:8001/
    record: true
`

func TestStaticConfig(t *testing.T) {
	cs, err := loadStaticConfig(strings.NewReader(staticYAML))
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].ID != "embed-a" || cs[0].Model != "example/model" || cs[0].URL != "http://localhost:8001" || cs[0].Interval != 1000 || cs[0].MetricsPath != "/metrics" {
		t.Fatal(cs)
	}
	for _, invalid := range []string{
		strings.Replace(staticYAML, "backend: tei", "backend: unknown", 1),
		strings.Replace(staticYAML, "task: embedding", "task: generation", 1),
		strings.Replace(staticYAML, "model: example/model", "model: ''", 1),
		staticYAML + "    typo: value\n",
		staticYAML + strings.TrimPrefix(staticYAML, "endpoints:\n"),
		staticYAML + "---\nendpoints: []\n",
		"foo: []\n",
	} {
		if _, err := loadStaticConfig(strings.NewReader(invalid)); err == nil {
			t.Fatalf("accepted invalid YAML: %s", invalid)
		}
	}
	if cs, err := loadStaticConfig(strings.NewReader("endpoints: []\n")); err != nil || len(cs) != 0 {
		t.Fatal(cs, err)
	}
}

func TestStaticCollectionNoDiscovery(t *testing.T) {
	discovery := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			discovery++
			http.Error(w, "unexpected metadata request", 500)
			return
		}
		fmt.Fprint(w, "# TYPE vllm:prompt_tokens_total counter\nvllm:prompt_tokens_total{model_name=\"unexpected\"} 10\n")
	}))
	defer server.Close()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := &App{db: db, client: server.Client(), staticConfig: true}
	w := &Worker{cfg: Config{ID: "static", URL: server.URL, MetricsPath: "/metrics", Interval: 1000, Backend: "vllm", Task: "reranker", Model: "configured"}, agg: map[string]*Aggregate{}}
	a.collect(context.Background(), w)
	a.collect(context.Background(), w)
	if discovery != 0 || w.state.Model != "configured" || w.state.Run == 0 || w.state.Status != "online" {
		t.Fatal(discovery, w.state)
	}
	for _, handler := range []http.HandlerFunc{a.add, a.update, a.remove, a.scan} {
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest("POST", "/", nil))
		if rec.Code != http.StatusForbidden {
			t.Fatal(rec.Code)
		}
	}
}
