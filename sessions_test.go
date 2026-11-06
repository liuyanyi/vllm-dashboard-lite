package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(`CREATE TABLE runs(id INTEGER PRIMARY KEY,endpoint_id TEXT NOT NULL,start TEXT NOT NULL,model TEXT NOT NULL DEFAULT '',metadata TEXT NOT NULL DEFAULT '',first_seen TEXT NOT NULL,last_seen TEXT NOT NULL,UNIQUE(endpoint_id,start));
 INSERT INTO runs VALUES(41,'e','123','old','{}','2026-01-01','2026-01-02');
 CREATE TABLE rollups(id INTEGER PRIMARY KEY,run_id INTEGER NOT NULL,at TEXT NOT NULL,data TEXT NOT NULL);
 INSERT INTO rollups VALUES(1,41,'2026-01-01','{}'); PRAGMA user_version=1;`)
	if err != nil {
		t.Fatal(err)
	}
	old.Close()
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	var id int
	var model, reason string
	if err = db.QueryRow("SELECT runs.id,model,reason FROM runs JOIN rollups ON runs.id=rollups.run_id").Scan(&id, &model, &reason); err != nil || id != 41 || model != "old" || reason != "legacy" {
		t.Fatal(id, model, reason, err)
	}
	a, err := createSession(db, "e", "123", "new", "", "manual", time.Now())
	if err != nil || a == 41 {
		t.Fatal(a, err)
	}
	b, err := createSession(db, "e", "", "new", "", "manual", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c, err := createSession(db, "e", "", "new", "", "manual", time.Now())
	if err != nil || b == c {
		t.Fatal(b, c, err)
	}
	db.Close()
	db, err = openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	db.QueryRow("SELECT count(*) FROM runs").Scan(&n)
	if n != 4 {
		t.Fatal(n)
	}
}

func TestObservationSessions(t *testing.T) {
	model, root, start := "modelA", "/weights/a", ""
	offline, metadataFail := false, false
	count, modelCalls := 100, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline {
			http.Error(w, "offline", 503)
			return
		}
		if r.URL.Path == "/v1/models" {
			modelCalls++
			if metadataFail {
				http.Error(w, "error", 500)
				return
			}
			fmt.Fprintf(w, `{"data":[{"id":%q,"root":%q,"created":%d}]}`, model, root, time.Now().UnixNano())
			return
		}
		if start != "" {
			fmt.Fprintf(w, "process_start_time_seconds %s\n", start)
		}
		fmt.Fprintf(w, "# TYPE vllm:prompt_tokens_total counter\nvllm:prompt_tokens_total{model_name=%q} %d\n", model, count)
	}))
	defer server.Close()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := &App{db: db, client: server.Client(), workers: map[string]*Worker{}}
	w := &Worker{cfg: Config{ID: "e", URL: server.URL, MetricsPath: "/metrics", Interval: 1000, Record: true}, agg: map[string]*Aggregate{}}
	collect := func() {
		t.Helper()
		a.collect(context.Background(), w)
		if w.state.Error != "" && !offline {
			t.Fatal(w.state.Error)
		}
	}
	collect()
	first := w.state.Run
	if first == 0 || w.state.Start != "" || w.state.Model != "modelA" || w.state.Status != "online" {
		t.Fatal(w.state)
	}
	count += 10
	collect()
	if w.state.Run != first || modelCalls != 1 {
		t.Fatal(w.state.Run, modelCalls)
	}
	// A root-only switch is discovered by the periodic metadata probe.
	w.retry = time.Time{}
	root = "/weights/b"
	collect()
	if w.state.Run == first || w.state.Reason != "model_changed" {
		t.Fatal(w.state)
	}
	var history int
	db.QueryRow("SELECT count(*) FROM rollups WHERE run_id=?", first).Scan(&history)
	if history == 0 {
		t.Fatal("missing history without process start")
	}
	second := w.state.Run
	w.retry = time.Time{}
	collect() // created changes are ignored
	if w.state.Run != second {
		t.Fatal("volatile metadata split")
	}
	w.retry = time.Time{}
	metadataFail = true
	collect()
	if w.state.Run != second || w.state.Model != "modelA" {
		t.Fatal("metadata failure lost identity")
	}
	metadataFail = false
	offline = true
	collect()
	offline = false
	collect()
	if w.state.Run != second {
		t.Fatal("brief gap split")
	}
	offline = true
	collect()
	w.offlineSince = time.Now().Add(-31 * time.Second)
	offline = false
	collect()
	if w.state.Run == second || w.state.Reason != "reconnected" {
		t.Fatal(w.state)
	}
	third := w.state.Run
	count = 1
	collect()
	if w.state.Run != third {
		t.Fatal("counter reset split")
	}
	start = "123"
	collect()
	if w.state.Run != third {
		t.Fatal("newly available timestamp split")
	}
	start = "124"
	collect()
	if w.state.Run == third || w.state.Reason != "process_changed" {
		t.Fatal(w.state)
	}
	fourth := w.state.Run
	start = ""
	collect()
	if w.state.Run != fourth {
		t.Fatal("missing timestamp split")
	}
	model = "modelB"
	collect()
	if w.state.Run == fourth || w.state.Model != "modelB" {
		t.Fatal(w.state)
	}
	fifth := w.state.Run
	a.workers["e"] = w
	req := httptest.NewRequest("POST", "/api/endpoints/e/sessions", strings.NewReader("{}"))
	req.SetPathValue("id", "e")
	response := httptest.NewRecorder()
	a.newSession(response, req)
	if response.Code != 202 {
		t.Fatal(response.Code)
	}
	collect()
	if w.state.Run == fifth || w.state.Reason != "manual" {
		t.Fatal(w.state)
	}
	var oldModel string
	db.QueryRow("SELECT model FROM runs WHERE id=?", first).Scan(&oldModel)
	if oldModel != "modelA" {
		t.Fatal(oldModel)
	}
}

func TestModelOrderIgnored(t *testing.T) {
	reverse := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reverse {
			fmt.Fprint(w, `{"data":[{"id":"b","root":"/b"},{"id":"a","root":"/a"}]}`)
		} else {
			fmt.Fprint(w, `{"data":[{"id":"a","root":"/a"},{"id":"b","root":"/b"}]}`)
		}
	}))
	defer server.Close()
	a := &App{client: server.Client()}
	c := Config{URL: server.URL}
	_, one, _ := a.modelInfo(context.Background(), c, "")
	reverse = true
	_, two, _ := a.modelInfo(context.Background(), c, "")
	if one != two {
		t.Fatal(one, two)
	}
}

func TestDelayedFlushKeepsObservationTime(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	rid, err := createSession(db, "e", "", "m", "", "first_observed", at)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{db: db}
	w := &Worker{state: State{Run: rid, Latest: Point{At: at.UnixMilli()}}, agg: map[string]*Aggregate{"counter": {Name: "counter", Kind: "counter", Delta: 10, Duration: 1}}}
	if !a.flush(w) {
		t.Fatal("flush failed")
	}
	var saved string
	if err = db.QueryRow("SELECT at FROM rollups WHERE run_id=?", rid).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if saved != at.UTC().Format(time.RFC3339Nano) {
		t.Fatal("flush moved observations to reconnect time", saved)
	}
}
