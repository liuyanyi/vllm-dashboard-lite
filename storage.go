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
CREATE INDEX IF NOT EXISTS rollup_run ON rollups(run_id,at);`)
	if e != nil {
		d.Close()
		return nil, e
	}
	var version int
	if e = d.QueryRow("PRAGMA user_version").Scan(&version); e != nil {
		d.Close()
		return nil, e
	}
	if version < 2 {
		// Keep historical IDs so all existing rollups still refer to the same run.
		_, e = d.Exec(`BEGIN;
CREATE TABLE runs_v2(id INTEGER PRIMARY KEY,endpoint_id TEXT NOT NULL,start TEXT NOT NULL DEFAULT '',model TEXT NOT NULL DEFAULT '',metadata TEXT NOT NULL DEFAULT '',first_seen TEXT NOT NULL,last_seen TEXT NOT NULL,reason TEXT NOT NULL DEFAULT 'legacy');
INSERT INTO runs_v2(id,endpoint_id,start,model,metadata,first_seen,last_seen) SELECT id,endpoint_id,start,model,metadata,first_seen,last_seen FROM runs;
DROP TABLE runs;
ALTER TABLE runs_v2 RENAME TO runs;
CREATE INDEX run_endpoint ON runs(endpoint_id,id);
PRAGMA user_version=2;
COMMIT;`)
		if e != nil {
			d.Exec("ROLLBACK")
			d.Close()
			return nil, e
		}
	}
	return d, nil
}
func createSession(db *sql.DB, id, start, model, metadata, reason string, at time.Time) (int64, error) {
	now := at.UTC().Format(time.RFC3339Nano)
	r, e := db.Exec("INSERT INTO runs(endpoint_id,start,model,metadata,first_seen,last_seen,reason) VALUES(?,?,?,?,?,?,?)", id, start, model, metadata, now, now, reason)
	if e != nil {
		return 0, e
	}
	return r.LastInsertId()
}
func saveJSON(db *sql.DB, id string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = db.Exec("INSERT INTO endpoints VALUES(?,?) ON CONFLICT(id) DO UPDATE SET config=excluded.config", id, string(b))
	return e
}
