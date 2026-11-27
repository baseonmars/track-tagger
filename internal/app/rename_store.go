package app

import (
	"database/sql"
	"encoding/json"
	"path/filepath"

	"track-tagger/internal/catalog"
)

func (s *Store) copyTarget(completion, source, ordinary string) (string, error) {
	var target string
	err := s.db.QueryRow("SELECT target FROM copy_targets WHERE completion=? AND source=?", completion, source).Scan(&target)
	if err == sql.ErrNoRows {
		known, err := s.known(ordinary)
		if err != nil || known == "" {
			return "", err
		}
		return ordinary, nil
	}
	return target, err
}

func (s *Store) startRename(b *renameBatch) error {
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if !b.Undo {
		if _, err = tx.Exec("INSERT INTO jobs VALUES(?,?,?,?)", b.Job, b.Item.ID, "running", "reviewed filename and metadata update"); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("INSERT INTO rename_batches VALUES(?,?,?,?,?,?)", b.Key, b.Job, b.Source, b.Destination, b.Mode, data); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) updateRename(b *renameBatch) error {
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("UPDATE rename_batches SET plan=? WHERE id=?", data, b.Key)
	return err
}

func (s *Store) pendingRenames(source, destination string, mode Mode, job string) ([]renameBatch, error) {
	q := "SELECT plan FROM rename_batches WHERE source=? AND destination=? AND mode=? ORDER BY id"
	args := []any{source, destination, mode}
	if job != "" {
		q = "SELECT plan FROM rename_batches WHERE job=? ORDER BY id"
		args = []any{job}
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var batches []renameBatch
	for rows.Next() {
		var data []byte
		var b renameBatch
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &b); err != nil {
			return nil, err
		}
		batches = append(batches, b)
	}
	return batches, rows.Err()
}

func (s *Store) removeRename(key string) error {
	_, err := s.db.Exec("DELETE FROM rename_batches WHERE id=?", key)
	return err
}

func (s *Store) renamePending(source, recording string) (bool, error) {
	var pending bool
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM rename_batches JOIN jobs ON rename_batches.job=jobs.id
		WHERE source=? AND recording=?)`, source, recording).Scan(&pending)
	return pending, err
}

func (s *Store) renameLocation(job string) (string, string, error) {
	var source, path string
	err := s.db.QueryRow("SELECT source,path FROM rename_history WHERE job=?", job).Scan(&source, &path)
	if err == sql.ErrNoRows {
		err = nil
	}
	return source, path, err
}

func (s *Store) finishRename(b *renameBatch, fresh catalog.Recording) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, o := range b.Outputs {
		if o.Prior == "" || o.Prior == o.Target {
			continue
		}
		if _, err = tx.Exec("DELETE FROM outputs WHERE path=?", o.Prior); err != nil {
			return err
		}
	}
	for _, o := range b.Outputs {
		if err = upsertOutput(tx, o.Target, o.After); err != nil {
			return err
		}
	}

	if b.Undo {
		if _, err = tx.Exec("UPDATE files SET status='undone' WHERE job=? AND status='committed' AND length(metadata)>0", b.Job); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT OR IGNORE INTO holds VALUES(?)", b.Item.ID); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE jobs SET status='undone' WHERE id=?", b.Job); err != nil {
			return err
		}
	} else {
		if err = s.finishRenameApply(tx, b, fresh); err != nil {
			return err
		}
	}
	b.Phase = "committed"
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE rename_batches SET plan=? WHERE id=?", data, b.Key); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) finishRenameApply(tx *sql.Tx, b *renameBatch, fresh catalog.Recording) error {
	item := b.Item
	completion := string(b.Mode) + "|" + b.Destination + "|" + item.ID
	actualPaths := make(map[string]string)
	for _, t := range fresh.Tracks {
		actualPaths[renamePathKey(t.Path)] = t.Path
	}
	targets := make(map[string]string, len(b.Outputs))
	for _, o := range b.Outputs {
		targets[o.Source] = o.Target
		if o.Stage != "" {
			before := o.Before
			if o.Companion && before == "" {
				// Undo restores the original source notes into a newly tagged copy.
				before = hashBytes(o.Metadata)
			}
			j := FileJournal{JobID: b.Job, Source: o.Source, Target: o.Target, BeforeHash: before, AfterHash: o.After, Metadata: o.Metadata, Companion: o.Companion, Mode: o.FileMode, SourceHash: o.SourceHash}
			if _, err := journalFile(tx, j, "committed"); err != nil {
				return err
			}
		}
		if b.Mode == Copy {
			if _, err := tx.Exec("INSERT INTO copy_targets VALUES(?,?,?) ON CONFLICT(completion,source) DO UPDATE SET target=excluded.target", completion, o.Source, o.Target); err != nil {
				return err
			}
		}
	}
	if b.Mode == Write {
		item.Tracks = append([]catalog.Track(nil), item.Tracks...)
		for i, t := range item.Tracks {
			path := actualPaths[renamePathKey(targets[t.Path])]
			item.Tracks[i].Path = path
			item.Tracks[i].RelativePath, _ = filepath.Rel(b.Source, path)
		}
		if _, err := tx.Exec("INSERT INTO rename_history VALUES(?,?,?)", b.Job, b.Source, item.Path); err != nil {
			return err
		}
	}
	item.Fingerprint, item.Evidence = fresh.Fingerprint, fresh.Evidence
	ev := evidence(fresh)
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO reviews VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET evidence=excluded.evidence,plan=excluded.plan", item.ID, ev, data); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO completions VALUES(?,?) ON CONFLICT(id) DO UPDATE SET evidence=excluded.evidence", completion, ev); err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE jobs SET status='completed' WHERE id=?", b.Job)
	return err
}
