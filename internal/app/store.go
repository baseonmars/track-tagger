package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	lock *flock.Flock
}
type Job struct{ ID, RecordingID, Status, Summary string }
type FileJournal struct {
	UndoOf                                               int64
	ID                                                   int64
	JobID, Source, Target, BeforeHash, AfterHash, Status string
	Metadata                                             []byte
}

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(path, "history.sqlite"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, lock: flock.New(filepath.Join(path, "writer.lock"))}
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS jobs(id TEXT PRIMARY KEY,recording TEXT NOT NULL,status TEXT NOT NULL,summary TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS files(id INTEGER PRIMARY KEY,job TEXT NOT NULL,source TEXT NOT NULL,target TEXT NOT NULL,before_hash TEXT NOT NULL,after_hash TEXT NOT NULL,metadata BLOB,status TEXT NOT NULL,undo_of INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS outputs(path TEXT PRIMARY KEY,hash TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS holds(id TEXT PRIMARY KEY);
 CREATE TABLE IF NOT EXISTS completions(id TEXT PRIMARY KEY,evidence TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS reviews(id TEXT PRIMARY KEY,evidence TEXT NOT NULL,plan BLOB NOT NULL);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) acquire() (func(), error) {
	ok, err := s.lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("another track-tagger writer is active")
	}
	return func() { _ = s.lock.Unlock() }, nil
}
func (s *Store) newJob(rec, summary string) (string, error) {
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	_, err := s.db.Exec("INSERT INTO jobs VALUES(?,?,?,?)", id, rec, "running", summary)
	return id, err
}
func (s *Store) jobStatus(id, status string) error {
	_, err := s.db.Exec("UPDATE jobs SET status=? WHERE id=?", status, id)
	return err
}
func (s *Store) journal(j FileJournal) (int64, error) {
	r, err := s.db.Exec("INSERT INTO files(job,source,target,before_hash,after_hash,metadata,status,undo_of) VALUES(?,?,?,?,?,?,?,?)", j.JobID, j.Source, j.Target, j.BeforeHash, j.AfterHash, j.Metadata, journalStatus(j), j.UndoOf)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}
func (s *Store) finishFile(id int64, target, hash, status string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE files SET status=? WHERE id=?", status, id); err != nil {
		return err
	}
	if err = upsertOutput(tx, target, hash); err != nil {
		return err
	}
	return tx.Commit()
}
func upsertOutput(tx *sql.Tx, path, hash string) error {
	_, err := tx.Exec("INSERT INTO outputs VALUES(?,?) ON CONFLICT(path) DO UPDATE SET hash=excluded.hash", path, hash)
	return err
}
func (s *Store) known(path string) (string, error) {
	var h string
	err := s.db.QueryRow("SELECT hash FROM outputs WHERE path=?", path).Scan(&h)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return h, err
}

// finishCompletion records verified unchanged outputs and their recording evidence together.
func (s *Store) finishCompletion(id, evidence string, skipped map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for path, hash := range skipped {
		if err = upsertOutput(tx, path, hash); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("INSERT INTO completions VALUES(?,?) ON CONFLICT(id) DO UPDATE SET evidence=excluded.evidence", id, evidence); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Files(job string) ([]FileJournal, error) { return s.files(job, false) }
func (s *Store) pendingFiles() ([]FileJournal, error)    { return s.files("", true) }
func (s *Store) files(job string, pending bool) ([]FileJournal, error) {
	q := "SELECT id,job,source,target,before_hash,after_hash,metadata,status,undo_of FROM files"
	var conds []string
	args := []any{}
	if job != "" {
		conds = append(conds, "job=?")
		args = append(args, job)
	}
	if pending {
		conds = append(conds, "status IN ('staged','undo-staged')")
	}
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY id DESC"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileJournal
	for rows.Next() {
		var j FileJournal
		if err = rows.Scan(&j.ID, &j.JobID, &j.Source, &j.Target, &j.BeforeHash, &j.AfterHash, &j.Metadata, &j.Status, &j.UndoOf); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
func (s *Store) History() ([]Job, error) {
	rows, err := s.db.Query("SELECT id,recording,status,summary FROM jobs ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		var j Job
		if err = rows.Scan(&j.ID, &j.RecordingID, &j.Status, &j.Summary); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
func (s *Store) saveReview(id, evidence string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO reviews VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET evidence=excluded.evidence,plan=excluded.plan", id, evidence, b)
	return err
}
func (s *Store) review(id string, v any) (string, bool, error) {
	var ev string
	var b []byte
	err := s.db.QueryRow("SELECT evidence,plan FROM reviews WHERE id=?", id).Scan(&ev, &b)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	err = json.Unmarshal(b, v)
	return ev, true, err
}

func journalStatus(j FileJournal) string {
	if j.UndoOf > 0 {
		return "undo-staged"
	}
	return "staged"
}
func (s *Store) finishUndo(id, original int64, target, hash string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE files SET status='undo' WHERE id=?", id); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE files SET status='undone' WHERE id=?", original); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT OR IGNORE INTO holds SELECT recording FROM jobs JOIN files ON files.job=jobs.id WHERE files.id=?", original); err != nil {
		return err
	}
	if err = upsertOutput(tx, target, hash); err != nil {
		return err
	}
	return tx.Commit()
}

// saveReviewDecision publishes the review, completion invalidation and undo-hold
// removal together so a failed save cannot change processing eligibility.
func (s *Store) saveReviewDecision(id, evidence string, item Item, completion string, changed bool) error {
	b, err := json.Marshal(item)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO reviews VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET evidence=excluded.evidence,plan=excluded.plan", id, evidence, b); err != nil {
		return err
	}
	if changed {
		if _, err = tx.Exec("DELETE FROM completions WHERE id=?", completion); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("DELETE FROM holds WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
