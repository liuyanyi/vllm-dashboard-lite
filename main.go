package main

import (
	"bufio"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed web/dist/*
var web embed.FS

type Config struct {
	ID          string `json:"id" yaml:"id"`
	URL         string `json:"url" yaml:"url"`
	Name        string `json:"name" yaml:"name"`
	Token       string `json:"token,omitempty" yaml:"token"`
	Record      bool   `json:"record" yaml:"record"`
	Interval    int    `json:"interval" yaml:"interval"`
	MetricsPath string `json:"metrics_path" yaml:"metrics_path"`
	Backend     string `json:"backend" yaml:"backend"`
	Task        string `json:"task" yaml:"task"`
	Model       string `json:"model,omitempty" yaml:"model"`
}
type Point struct {
	At     int64               `json:"at"`
	Values map[string]*float64 `json:"values"`
}
type State struct {
	Config      Config   `json:"config"`
	Status      string   `json:"status"`
	Error       string   `json:"error,omitempty"`
	Run         int64    `json:"run"`
	Start       string   `json:"start"`
	StartStatus string   `json:"start_status"`
	FirstSeen   string   `json:"first_seen"`
	Reason      string   `json:"reason"`
	Model       string   `json:"model"`
	Latest      Point    `json:"latest"`
	Points      []Point  `json:"points"`
	Raw         []Series `json:"raw,omitempty"`
}
type Worker struct {
	mu           sync.Mutex
	cfg          Config
	state        State
	prev         Snapshot
	recent       []Snapshot
	agg          map[string]*Aggregate
	window       time.Time
	offlineSince time.Time
	touched      time.Time
	manual       bool
}
type App struct {
	mu      sync.Mutex
	workers map[string]*Worker
	order   []string
	db      *sql.DB
	client  *http.Client
	ctx     context.Context
}

func main() {
	addr := flag.String("listen", "127.0.0.1:9090", "listen address")
	path := flag.String("db", "monitor.db", "SQLite file")
	desktopChild := flag.Bool("desktop-child", false, "stop when desktop stdin closes")
	defaultConfig := os.Getenv("VLLM_DASHBOARD_CONFIG")
	if defaultConfig == "" {
		defaultConfig = "monitor.yaml"
	}
	configPath := flag.String("config", defaultConfig, "YAML endpoint configuration (required)")
	flag.Parse()
	file, err := os.Open(*configPath)
	if err != nil {
		log.Fatalf("open YAML config %q: %v (copy config.example.yaml to monitor.yaml)", *configPath, err)
	}
	configs, err := loadStaticConfig(file)
	file.Close()
	if err != nil {
		log.Fatal(err)
	}
	db, e := openDB(*path)
	if e != nil {
		log.Fatal(e)
	}
	defer db.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *desktopChild {
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				if scanner.Text() == "shutdown" {
					break
				}
			}
			cancel()
		}()
	}
	a := &App{workers: map[string]*Worker{}, db: db, ctx: ctx, client: &http.Client{Timeout: 4 * time.Second}}
	for _, c := range configs {
		a.start(c)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/settings", a.settings)
	mux.HandleFunc("GET /api/endpoints", a.list)
	mux.HandleFunc("POST /api/endpoints/{id}/sessions", a.newSession)
	mux.HandleFunc("GET /api/events", a.events)
	mux.HandleFunc("GET /api/runs", a.runs)
	mux.HandleFunc("GET /api/history", a.history)
	sub, _ := fs.Sub(web, "web/dist")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	srv := &http.Server{Addr: *addr, Handler: protect(mux), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		c, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		srv.Shutdown(c)
	}()
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("vLLM Monitor http://%s", listener.Addr().String())
	if *desktopChild {
		fmt.Printf("MONITOR_READY http://%s\n", listener.Addr().String())
	}
	if e = srv.Serve(listener); e != http.ErrServerClosed {
		log.Fatal(e)
	}
	a.mu.Lock()
	for _, w := range a.workers {
		w.mu.Lock()
		a.flush(w)
		w.mu.Unlock()
	}
	a.mu.Unlock()
}
func protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" {
			u, e := url.Parse(o)
			if e != nil || u.Host != r.Host {
				http.Error(w, "cross-origin request rejected", 403)
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}
func jsonOut(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, e error) { http.Error(w, e.Error(), 400) }
func valid(c *Config) error {
	c.Model = strings.TrimSpace(c.Model)
	if c.Backend != "vllm" && c.Backend != "tei" {
		return fmt.Errorf("backend must be vllm or tei")
	}
	if c.Task != "generation" && c.Task != "embedding" && c.Task != "reranker" {
		return fmt.Errorf("task must be generation, embedding or reranker")
	}
	if c.Model == "" {
		return fmt.Errorf("model is required")
	}
	if c.Backend == "tei" && c.Task == "generation" {
		return fmt.Errorf("TEI requires a configured model and embedding or reranker task")
	}
	u, e := url.Parse(c.URL)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("请输入有效 http(s) 服务地址")
	}
	c.URL = strings.TrimRight(c.URL, "/")
	if c.Interval == 0 {
		c.Interval = 1000
	}
	if c.Interval < 500 || c.Interval > 10000 {
		return fmt.Errorf("刷新间隔须为 500–10000 ms")
	}
	if c.MetricsPath == "" {
		c.MetricsPath = "/metrics"
	}
	if !strings.HasPrefix(c.MetricsPath, "/") || strings.HasPrefix(c.MetricsPath, "//") {
		return fmt.Errorf("metrics 路径须以单个 / 开头")
	}
	return nil
}
func (a *App) start(c Config) {
	ctx := a.ctx
	w := &Worker{cfg: c, agg: map[string]*Aggregate{}, state: State{Status: "connecting", Model: c.Model, Points: []Point{}}}
	a.workers[c.ID] = w
	a.order = append(a.order, c.ID)
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			a.collect(ctx, w)
			w.mu.Lock()
			d := w.cfg.Interval
			w.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(d) * time.Millisecond):
			}
		}
	}()
}
func (a *App) get(ctx context.Context, c Config, path string) (*http.Response, error) {
	r, e := http.NewRequestWithContext(ctx, "GET", c.URL+path, nil)
	if e != nil {
		return nil, e
	}
	if c.Token != "" {
		r.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, e := a.client.Do(r)
	if e != nil {
		return nil, fmt.Errorf("连接失败")
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return res, nil
}
func (a *App) snapshot(ctx context.Context, c Config) (Snapshot, error) {
	res, e := a.get(ctx, c, c.MetricsPath)
	if e != nil {
		return Snapshot{}, e
	}
	defer res.Body.Close()
	return parseMetrics(io.LimitReader(res.Body, 8<<20))
}
func (a *App) collect(ctx context.Context, w *Worker) {
	w.mu.Lock()
	c := w.cfg
	w.mu.Unlock()
	s, e := a.snapshot(ctx, c)
	w.mu.Lock()
	if ctx.Err() != nil {
		w.mu.Unlock()
		return
	}
	if e != nil {
		if w.offlineSince.IsZero() {
			w.offlineSince = time.Now()
			if w.state.Run > 0 && !w.prev.At.IsZero() {
				a.db.Exec("UPDATE runs SET last_seen=? WHERE id=?", w.prev.At.UTC().Format(time.RFC3339Nano), w.state.Run)
			}
		}
		w.state.Status = "offline"
		w.state.Error = e.Error()
		w.prev = Snapshot{}
		w.recent = nil
		w.state.Points = append(w.state.Points, Point{At: time.Now().UnixMilli(), Values: map[string]*float64{}})
		if len(w.state.Points) > 3600 {
			w.state.Points = w.state.Points[len(w.state.Points)-3600:]
		}
		w.mu.Unlock()
		return
	}
	w.state.Error = ""
	w.state.Status = "online"
	w.state.StartStatus = s.StartStatus
	reason := sessionReason(w, s)
	if reason != "" {
		if !a.flush(w) {
			w.state.Error = "历史写入失败"
			w.mu.Unlock()
			return
		}
		if w.state.Run > 0 && !w.prev.At.IsZero() {
			a.db.Exec("UPDATE runs SET last_seen=? WHERE id=?", w.prev.At.UTC().Format(time.RFC3339Nano), w.state.Run)
		}
		rid, err := createSession(a.db, c.ID, s.Start, c.Model, "", reason, s.At)
		if err != nil {
			w.state.Error = "无法保存会话"
			w.mu.Unlock()
			return
		}
		w.state.Run, w.state.Model, w.state.Start = rid, c.Model, s.Start
		w.state.FirstSeen, w.state.Reason = s.At.UTC().Format(time.RFC3339Nano), reason
		w.prev, w.recent, w.state.Points = Snapshot{}, nil, []Point{}
		w.manual = false
		w.touched = s.At
	} else {
		if s.Start != "" {
			w.state.Start = s.Start
		}
		if s.At.Sub(w.touched) >= 30*time.Second {
			if _, err := a.db.Exec("UPDATE runs SET start=?,last_seen=? WHERE id=?", w.state.Start, s.At.UTC().Format(time.RFC3339Nano), w.state.Run); err != nil {
				w.state.Error = "会话更新失败"
			} else {
				w.touched = s.At
			}
		}
	}
	w.offlineSince = time.Time{}
	maxGap := float64(c.Interval)/1000*3 + 1
	if c.Record && w.state.Run > 0 && !w.prev.At.IsZero() {
		if len(w.agg) == 0 {
			w.window = w.prev.At
		}
		accumulate(w.agg, w.prev, s, maxGap)
	}
	if !w.prev.At.IsZero() && s.At.Sub(w.prev.At).Seconds() > maxGap {
		w.recent = nil
	}
	w.recent = append(w.recent, s)
	for len(w.recent) > 1 && s.At.Sub(w.recent[0].At) > 60*time.Second {
		w.recent = w.recent[1:]
	}
	rateAgg := map[string]*Aggregate{}
	latAgg := map[string]*Aggregate{}
	for i := 1; i < len(w.recent); i++ {
		accumulate(latAgg, w.recent[i-1], w.recent[i], maxGap)
		if s.At.Sub(w.recent[i-1].At) <= 10*time.Second {
			accumulate(rateAgg, w.recent[i-1], w.recent[i], maxGap)
		}
	}
	vals := summaries(rateAgg, s)
	for k, v := range summaries(latAgg, s) {
		if strings.Contains(k, "_seconds") {
			vals[k] = v
		}
	}
	pt := Point{s.At.UnixMilli(), vals}
	w.state.Latest = pt
	w.state.Points = append(w.state.Points, pt)
	cut := s.At.Add(-30 * time.Minute).UnixMilli()
	for len(w.state.Points) > 0 && w.state.Points[0].At < cut {
		w.state.Points = w.state.Points[1:]
	}
	w.state.Raw = nil
	for _, v := range s.Values {
		w.state.Raw = append(w.state.Raw, v)
	}
	w.prev = s
	if len(w.agg) > 0 && s.At.Sub(w.window) >= 30*time.Second {
		if !a.flush(w) {
			w.state.Error = "历史写入失败"
		}
	}
	w.mu.Unlock()
}
func (a *App) flush(w *Worker) bool {
	if len(w.agg) == 0 {
		return true
	}
	b, e := json.Marshal(w.agg)
	if e != nil {
		return false
	}
	// A delayed flush after an outage must keep the sample's time, not the
	// reconnection time, otherwise yesterday's values appear in today's history.
	at := time.Now()
	if w.state.Latest.At > 0 {
		at = time.UnixMilli(w.state.Latest.At)
	}
	if _, e = a.db.Exec("INSERT INTO rollups(run_id,at,data) VALUES(?,?,?)", w.state.Run, at.UTC().Format(time.RFC3339Nano), string(b)); e != nil {
		return false
	}
	a.db.Exec("DELETE FROM rollups WHERE at < ?", time.Now().Add(-7*24*time.Hour).UTC().Format(time.RFC3339Nano))
	w.agg = map[string]*Aggregate{}
	return true
}
func (a *App) states(full bool) []State {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []State{}
	for _, w := range a.workers {
		w.mu.Lock()
		s := w.state
		s.Config = w.cfg
		s.Config.Token = ""
		if full {
			s.Points = append([]Point{}, s.Points...)
			s.Raw = append([]Series{}, s.Raw...)
		} else {
			s.Points = nil
			s.Raw = nil
		}
		out = append(out, s)
		w.mu.Unlock()
	}
	order := make(map[string]int, len(a.order))
	for i, id := range a.order {
		order[id] = i
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := out[i].Config.ID, out[j].Config.ID
		if order[left] != order[right] {
			return order[left] < order[right]
		}
		return left < right
	})
	return out
}
func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	jsonOut(w, map[string]bool{"static_config": true})
}
func (a *App) list(w http.ResponseWriter, r *http.Request) { jsonOut(w, a.states(true)) }
func (a *App) events(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		b, _ := json.Marshal(a.states(false))
		http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, e := fmt.Fprintf(w, "data: %s\n\n", b); e != nil {
			return
		}
		f.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-a.ctx.Done():
			return
		case <-t.C:
		}
	}
}
func (a *App) runs(w http.ResponseWriter, r *http.Request) {
	rows, e := a.db.Query("SELECT id,start,model,first_seen,last_seen,reason FROM runs WHERE endpoint_id=? ORDER BY id DESC LIMIT 100", r.URL.Query().Get("endpoint"))
	if e != nil {
		fail(w, e)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var s, m, f, l, reason string
		rows.Scan(&id, &s, &m, &f, &l, &reason)
		out = append(out, map[string]any{"id": id, "start": s, "model": m, "first_seen": f, "last_seen": l, "reason": reason})
	}
	jsonOut(w, out)
}
func (a *App) history(w http.ResponseWriter, r *http.Request) {
	rows, e := a.db.Query("SELECT at,data FROM (SELECT at,data FROM rollups WHERE run_id=? ORDER BY at DESC LIMIT 20160) ORDER BY at", r.URL.Query().Get("run"))
	if e != nil {
		fail(w, e)
		return
	}
	defer rows.Close()
	out := []Point{}
	for rows.Next() {
		var at, raw string
		rows.Scan(&at, &raw)
		var ag map[string]*Aggregate
		json.Unmarshal([]byte(raw), &ag)
		s := Snapshot{Values: map[string]Series{}}
		for k, g := range ag {
			if g.Kind == "gauge" && g.Duration > 0 {
				s.Values[k] = Series{g.Name, g.Labels, g.Kind, g.Integral / g.Duration}
			}
		}
		t, _ := time.Parse(time.RFC3339Nano, at)
		out = append(out, Point{t.UnixMilli(), summaries(ag, s)})
	}
	jsonOut(w, out)
}
