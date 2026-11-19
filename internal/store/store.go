// Package store remembers postings across runs so the model is not shown the
// same job twice.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"jobradar/internal/job"
)

const (
	Approved = "approved"
	Rejected = "rejected"
)

type Store struct {
	db *sql.DB
}

const schema = `
create table if not exists jobs (
	key         text primary key,
	source      text not null,
	provider_id text not null,
	url_key     text not null,
	url         text not null,
	title       text not null default '',
	company     text not null default '',
	location    text not null default '',
	posted      text not null default '',
	decision    text,
	decided_at  text
);
create index if not exists jobs_url_key on jobs (url_key);
`

// Open opens or creates the database file.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create schema: %v", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Blocked reports whether a posting was already decided. A row with no decision
// is not blocked, so a posting that was shown but never judged can come back.
func (s *Store) Blocked(ctx context.Context, key, urlKey string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `
		select 1 from jobs
		where decision is not null and (key = ? or url_key = ?)
		limit 1`, key, urlKey).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Touch records that a posting was shown, so save can find it by key later. The
// upsert refreshes the posting fields and leaves decision alone.
func (s *Store) Touch(ctx context.Context, postings []job.Job) error {
	if len(postings) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, `
		insert into jobs (key, source, provider_id, url_key, url, title, company, location, posted)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict (key) do update set
			url      = excluded.url,
			title    = excluded.title,
			company  = excluded.company,
			location = excluded.location,
			posted   = excluded.posted`)
	if err != nil {
		return err
	}
	defer func() {
		_ = stmt.Close()
	}()

	for _, posting := range postings {
		if _, err := stmt.ExecContext(ctx,
			posting.Key, posting.Source, posting.ID, job.URLKey(posting.URL), posting.URL,
			posting.Title, posting.Company, posting.Location, posting.Posted,
		); err != nil {
			return fmt.Errorf("record %s: %v", posting.Key, err)
		}
	}

	return tx.Commit()
}

// Decide records the model's verdict for keys it saw. Unknown keys are ignored.
func (s *Store) Decide(ctx context.Context, keys []string, decision string) (int, error) {
	if decision != Approved && decision != Rejected {
		return 0, fmt.Errorf("unknown decision %q", decision)
	}
	if len(keys) == 0 {
		return 0, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx,
		`update jobs set decision = ?, decided_at = ? where key = ?`)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = stmt.Close()
	}()

	updated := 0
	for _, key := range keys {
		result, err := stmt.ExecContext(ctx, decision, now, key)
		if err != nil {
			return 0, fmt.Errorf("decide %s: %v", key, err)
		}
		if affected, err := result.RowsAffected(); err == nil {
			updated += int(affected)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return updated, nil
}

// ApprovedJobs returns the stored rows for keys the model approved, so the tool
// can hand back the shortlist without another provider call.
func (s *Store) ApprovedJobs(ctx context.Context, keys []string) ([]job.Job, error) {
	approved := make([]job.Job, 0, len(keys))
	for _, key := range keys {
		var item job.Job
		err := s.db.QueryRowContext(ctx, `
			select key, source, provider_id, url, title, company, location, posted
			from jobs where key = ? and decision = ?`, key, Approved).
			Scan(&item.Key, &item.Source, &item.ID, &item.URL, &item.Title,
				&item.Company, &item.Location, &item.Posted)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		approved = append(approved, item)
	}
	return approved, nil
}

func (s *Store) Counts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`select coalesce(decision, 'undecided'), count(*) from jobs group by 1`)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	counts := map[string]int{"undecided": 0, Approved: 0, Rejected: 0}
	for rows.Next() {
		var decision string
		var count int
		if err := rows.Scan(&decision, &count); err != nil {
			return nil, err
		}
		counts[decision] = count
	}
	return counts, rows.Err()
}
