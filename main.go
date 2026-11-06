package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed web/dist/*
var web embed.FS

type Config struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Name        string `json:"name"`
	Token       string `json:"token,omitempty"`
	Record      bool   `json:"record"`
	Interval    int    `json:"interval"`
	MetricsPath string `json:"metrics_path"`
}
type Point struct {
	At     int64               `json:"at"`
	Values map[string]*float64 `json:"values"`
}
type State struct {
	Config Config   `json:"config"`
	Status string   `json:"status"`
	Error  string   `json:"error,omitempty"`
	Run    int64    `json:"run"`
	Start  string   `json:"start"`
	Model  string   `json:"model"`
	Latest Point    `json:"latest"`
	Points []Point  `json:"points"`
	Raw    []Series `json:"raw,omitempty"`
}
type Worker struct {
	mu     sync.Mutex
	cfg    Config
	state  State
	prev   Snapshot
	recent []Snapshot
	agg    map[string]*Aggregate
	window time.Time
	meta   bool
	retry  time.Time
	cancel context.CancelFunc
}
type App struct {
	mu      sync.Mutex
	workers map[string]*Worker
	db      *sql.DB
	client  *http.Client
	ctx     context.Context
}

func main() {
	addr := flag.String("listen", "127.0.0.1:9090", "listen address")
	path := flag.String("db", "monitor.db", "SQLite file")
	flag.Parse()
	db, e := openDB(*path)
	if e != nil {
		log.Fatal(e)
	}
	defer db.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	a := &App{workers: map[string]*Worker{}, db: db, ctx: ctx, client: &http.Client{Timeout: 4 * time.Second}}
	rows, e := db.Query("SELECT config FROM endpoints")
	if e != nil {
		log.Fatal(e)
	}
	var configs []Config
	for rows.Next() {
		var raw string
		rows.Scan(&raw)
		var c Config
		if json.Unmarshal([]byte(raw), &c) == nil {
			configs = append(configs, c)
		}
	}
	rows.Close()
	for _, c := range configs {
		a.start(c)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/endpoints", a.list)
	mux.HandleFunc("POST /api/endpoints", a.add)
	mux.HandleFunc("PATCH /api/endpoints/{id}", a.update)
	mux.HandleFunc("DELETE /api/endpoints/{id}", a.remove)
	mux.HandleFunc("GET /api/events", a.events)
	mux.HandleFunc("GET /api/runs", a.runs)
	mux.HandleFunc("GET /api/history", a.history)
	mux.HandleFunc("POST /api/scan", a.scan)
	sub, _ := fs.Sub(web, "web/dist")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	srv := &http.Server{Addr: *addr, Handler: protect(mux), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		c, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		srv.Shutdown(c)
	}()
	log.Printf("vLLM Monitor http://%s", *addr)
	if e = srv.ListenAndServe(); e != http.ErrServerClosed {
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
func readBody(w http.ResponseWriter, r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(v)
}
func valid(c *Config) error {
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
	ctx, cancel := context.WithCancel(a.ctx)
	w := &Worker{cfg: c, cancel: cancel, agg: map[string]*Aggregate{}, state: State{Status: "connecting", Points: []Point{}}}
	a.workers[c.ID] = w
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
	if s.Start != w.state.Start {
		if !a.flush(w) {
			w.state.Error = "历史写入失败"
			w.mu.Unlock()
			return
		}
		w.state.Start = s.Start
		w.state.Run = 0
		w.state.Model = s.Model
		w.meta = false
		w.retry = time.Time{}
		w.prev = Snapshot{}
		w.recent = nil
		w.state.Points = []Point{}
	}
	if s.Start != "" && w.state.Run == 0 {
		rid, m, meta, err := runIdentity(a.db, c.ID, s.Start, s.Model)
		if err != nil {
			w.state.Error = "无法保存批次"
		} else {
			w.state.Run = rid
			w.state.Model = m
			w.meta = meta != ""
		}
	}
	if s.Start == "" {
		w.state.Status = "realtime"
		w.state.Model = s.Model
	}
	maxGap := float64(c.Interval)/1000*3 + 1
	if c.Record && w.cfg.Record && w.state.Run > 0 && !w.prev.At.IsZero() {
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
	need := s.Start != "" && w.state.Run > 0 && !w.meta && time.Now().After(w.retry)
	rid := w.state.Run
	w.retry = time.Now().Add(15 * time.Second)
	w.mu.Unlock()
	if need {
		res, err := a.get(ctx, c, "/v1/models")
		if err != nil {
			return
		}
		var model struct {
			Data []struct {
				ID   string `json:"id"`
				Root string `json:"root"`
				Max  int    `json:"max_model_len"`
			} `json:"data"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&model)
		res.Body.Close()
		if err != nil || len(model.Data) == 0 {
			return
		}
		check, err := a.snapshot(ctx, c)
		if err != nil || check.Start != s.Start {
			return
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.state.Run != rid || ctx.Err() != nil {
			return
		}
		b, _ := json.Marshal(model)
		name := model.Data[0].ID
		for _, m := range model.Data {
			if m.ID == s.Model {
				name = m.ID
			}
		}
		if _, err = a.db.Exec("UPDATE runs SET model=?,metadata=? WHERE id=?", name, string(b), rid); err == nil {
			w.state.Model = name
			w.meta = true
		}
	}
}
func (a *App) flush(w *Worker) bool {
	if len(w.agg) == 0 {
		return true
	}
	b, e := json.Marshal(w.agg)
	if e != nil {
		return false
	}
	if _, e = a.db.Exec("INSERT INTO rollups(run_id,at,data) VALUES(?,?,?)", w.state.Run, time.Now().UTC().Format(time.RFC3339Nano), string(b)); e != nil {
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
	return out
}
func (a *App) list(w http.ResponseWriter, r *http.Request) { jsonOut(w, a.states(true)) }
func (a *App) add(w http.ResponseWriter, r *http.Request) {
	var c Config
	if e := readBody(w, r, &c); e != nil {
		fail(w, e)
		return
	}
	if e := valid(&c); e != nil {
		fail(w, e)
		return
	}
	b := make([]byte, 12)
	rand.Read(b)
	c.ID = hex.EncodeToString(b)
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, x := range a.workers {
		x.mu.Lock()
		same := x.cfg.URL == c.URL
		x.mu.Unlock()
		if same {
			fail(w, fmt.Errorf("端点已存在"))
			return
		}
	}
	if e := saveJSON(a.db, c.ID, c); e != nil {
		fail(w, e)
		return
	}
	a.start(c)
	c.Token = ""
	jsonOut(w, c)
}
func (a *App) update(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Record   *bool `json:"record"`
		Interval *int  `json:"interval"`
	}
	if e := readBody(w, r, &p); e != nil {
		fail(w, e)
		return
	}
	a.mu.Lock()
	x := a.workers[r.PathValue("id")]
	a.mu.Unlock()
	if x == nil {
		http.NotFound(w, r)
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	c := x.cfg
	if p.Record != nil {
		c.Record = *p.Record
	}
	if p.Interval != nil {
		c.Interval = *p.Interval
	}
	if e := valid(&c); e != nil {
		fail(w, e)
		return
	}
	if x.cfg.Record != c.Record {
		if !a.flush(x) {
			http.Error(w, "写入失败", 500)
			return
		}
		x.prev = Snapshot{}
	}
	if e := saveJSON(a.db, c.ID, c); e != nil {
		fail(w, e)
		return
	}
	x.cfg = c
	jsonOut(w, map[string]bool{"ok": true})
}
func (a *App) remove(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := r.PathValue("id")
	if x := a.workers[id]; x != nil {
		x.cancel()
		x.mu.Lock()
		a.flush(x)
		x.mu.Unlock()
		delete(a.workers, id)
	}
	a.db.Exec("DELETE FROM endpoints WHERE id=?", id)
	jsonOut(w, map[string]bool{"ok": true})
}
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
	rows, e := a.db.Query("SELECT id,start,model,first_seen,last_seen FROM runs WHERE endpoint_id=? ORDER BY id DESC LIMIT 100", r.URL.Query().Get("endpoint"))
	if e != nil {
		fail(w, e)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var s, m, f, l string
		rows.Scan(&id, &s, &m, &f, &l)
		out = append(out, map[string]any{"id": id, "start": s, "model": m, "first_seen": f, "last_seen": l})
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
func (a *App) scan(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Host  string `json:"host"`
		Ports string `json:"ports"`
	}
	if e := readBody(w, r, &p); e != nil {
		fail(w, e)
		return
	}
	if p.Host == "" || strings.ContainsAny(p.Host, "/ ?#@") {
		fail(w, fmt.Errorf("主机名无效"))
		return
	}
	ports := map[int]bool{}
	for _, part := range strings.Split(p.Ports, ",") {
		bounds := strings.Split(strings.TrimSpace(part), "-")
		lo, e := strconv.Atoi(bounds[0])
		hi := lo
		if len(bounds) == 2 {
			hi, e = strconv.Atoi(bounds[1])
		}
		if e != nil || len(bounds) > 2 || lo < 1 || hi > 65535 || hi < lo || hi-lo > 255 {
			fail(w, fmt.Errorf("端口范围无效，最多 256 个"))
			return
		}
		for i := lo; i <= hi; i++ {
			ports[i] = true
		}
	}
	if len(ports) > 256 {
		fail(w, fmt.Errorf("最多扫描 256 个端口"))
		return
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	out := []string{}
	for port := range ports {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-r.Context().Done():
				return
			}
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(r.Context(), 1200*time.Millisecond)
			defer cancel()
			c := Config{URL: "http://" + net.JoinHostPort(p.Host, strconv.Itoa(port)), MetricsPath: "/metrics"}
			s, e := a.snapshot(ctx, c)
			if e == nil {
				for _, v := range s.Values {
					if strings.HasPrefix(v.Name, "vllm:") {
						mu.Lock()
						out = append(out, c.URL)
						mu.Unlock()
						break
					}
				}
			}
		}(port)
	}
	wg.Wait()
	jsonOut(w, out)
}
