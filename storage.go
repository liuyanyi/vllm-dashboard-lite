package main

import (
	"database/sql"
	"encoding/json"
	_ "modernc.org/sqlite"
	"time"
)

func openDB(path string) (*sql.DB, error) {
	d, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	d.SetMaxOpenConns(1)
	_, e = d.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS endpoints(id TEXT PRIMARY KEY, config TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS runs(id INTEGER PRIMARY KEY, endpoint_id TEXT NOT NULL,start TEXT NOT NULL,model TEXT NOT NULL DEFAULT '',metadata TEXT NOT NULL DEFAULT '',first_seen TEXT NOT NULL,last_seen TEXT NOT NULL,UNIQUE(endpoint_id,start));
CREATE TABLE IF NOT EXISTS rollups(id INTEGER PRIMARY KEY,run_id INTEGER NOT NULL,at TEXT NOT NULL,data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS rollup_run ON rollups(run_id,at); PRAGMA user_version=1;`)
	return d, e
}
func runIdentity(db *sql.DB, id, start, model string) (int64, string, string, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, e := db.Exec("INSERT INTO runs(endpoint_id,start,model,first_seen,last_seen) VALUES(?,?,?,?,?) ON CONFLICT(endpoint_id,start) DO UPDATE SET last_seen=excluded.last_seen", id, start, model, now, now)
	if e != nil {
		return 0, "", "", e
	}
	var rid int64
	var m, meta string
	e = db.QueryRow("SELECT id,model,metadata FROM runs WHERE endpoint_id=? AND start=?", id, start).Scan(&rid, &m, &meta)
	return rid, m, meta, e
}
func saveJSON(db *sql.DB, id string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = db.Exec("INSERT INTO endpoints VALUES(?,?) ON CONFLICT(id) DO UPDATE SET config=excluded.config", id, string(b))
	return e
}
