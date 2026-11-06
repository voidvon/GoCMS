// Package geography owns a shared, versioned reference catalogue, independent
// of site content, payment providers, and order snapshots.
package geography

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed data/countryInfo.txt
var seedCountries string

var ErrBusy = errors.New("geography import already running")
var ErrInvalid = errors.New("invalid geography data")

const Source = "https://www.geonames.org/"
const License = "https://creativecommons.org/licenses/by/4.0/"

type Item struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Country    string `json:"country,omitempty"`
	Region     string `json:"region,omitempty"`
	RegionName string `json:"region_name,omitempty"`
	ASCII      string `json:"ascii,omitempty"`
}
type Dataset struct {
	ID        int64  `json:"id"`
	Version   string `json:"version"`
	Created   string `json:"created_at"`
	Countries int    `json:"countries"`
	Regions   int    `json:"regions"`
	Cities    int    `json:"cities"`
	Active    bool   `json:"active"`
}
type Job struct {
	Running bool   `json:"running"`
	Stage   string `json:"stage"`
	Error   string `json:"error,omitempty"`
}
type Status struct {
	Datasets []Dataset `json:"datasets"`
	Job      Job       `json:"job"`
	Source   string    `json:"source"`
	License  string    `json:"license"`
}
type Store struct {
	db       *sql.DB
	importMu sync.Mutex
	jobMu    sync.Mutex
	job      Job
	ctx      context.Context
	cancel   context.CancelFunc
	workers  sync.WaitGroup
}

func Open(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(filepath.Join(root, "geography.sqlite"))
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	database, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(4)
	ctx, cancel := context.WithCancel(context.Background())
	s := &Store{db: database, ctx: ctx, cancel: cancel}
	if _, err = database.Exec(`PRAGMA journal_mode=WAL;
 CREATE TABLE IF NOT EXISTS geo_dataset(id INTEGER PRIMARY KEY,version TEXT NOT NULL,created_at TEXT NOT NULL,countries INTEGER NOT NULL DEFAULT 0,regions INTEGER NOT NULL DEFAULT 0,cities INTEGER NOT NULL DEFAULT 0,active INTEGER NOT NULL DEFAULT 0);
 CREATE UNIQUE INDEX IF NOT EXISTS geo_one_active ON geo_dataset(active) WHERE active=1;
 CREATE TABLE IF NOT EXISTS geo_country(dataset INTEGER NOT NULL,code TEXT NOT NULL,name TEXT NOT NULL,PRIMARY KEY(dataset,code));
 CREATE TABLE IF NOT EXISTS geo_region(dataset INTEGER NOT NULL,country TEXT NOT NULL,code TEXT NOT NULL,name TEXT NOT NULL,ascii TEXT NOT NULL,PRIMARY KEY(dataset,country,code));
 CREATE TABLE IF NOT EXISTS geo_city(dataset INTEGER NOT NULL,id TEXT NOT NULL,country TEXT NOT NULL,region TEXT NOT NULL,name TEXT NOT NULL,ascii TEXT NOT NULL,search TEXT NOT NULL,population INTEGER NOT NULL,PRIMARY KEY(dataset,id));
 CREATE INDEX IF NOT EXISTS geo_city_lookup ON geo_city(dataset,country,region,population DESC);
 CREATE INDEX IF NOT EXISTS geo_city_country ON geo_city(dataset,country,population DESC);`); err != nil {
		s.Close()
		return nil, err
	}
	var count int
	if err = database.QueryRow(`SELECT COUNT(*) FROM geo_dataset WHERE active=1`).Scan(&count); err == nil && count == 0 {
		err = s.importFiles(ctx, "Built-in countries", strings.NewReader(seedCountries), nil, nil)
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	s.setJob(Job{})
	return s, nil
}
func (s *Store) Close() error {
	s.cancel()
	// Serialize shutdown with Start before waiting, so no worker can be added
	// concurrently with Wait or after the database has closed.
	s.importMu.Lock()
	defer s.importMu.Unlock()
	s.workers.Wait()
	return s.db.Close()
}
func (s *Store) setJob(j Job)       { s.jobMu.Lock(); s.job = j; s.jobMu.Unlock() }
func (s *Store) Stage(stage string) { s.setJob(Job{Running: true, Stage: stage}) }
func (s *Store) Start(work func(context.Context) error) error {
	if !s.importMu.TryLock() {
		return ErrBusy
	}
	if err := s.ctx.Err(); err != nil {
		s.importMu.Unlock()
		return err
	}
	s.Stage("准备导入")
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		defer s.importMu.Unlock()
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Minute)
		defer cancel()
		if err := work(ctx); err != nil {
			s.setJob(Job{Stage: "导入失败", Error: err.Error()})
		} else {
			s.setJob(Job{Stage: "导入完成"})
		}
	}()
	return nil
}
func (s *Store) Status(ctx context.Context) (Status, error) {
	result := Status{Datasets: []Dataset{}, Source: Source, License: License}
	s.jobMu.Lock()
	result.Job = s.job
	s.jobMu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT id,version,created_at,countries,regions,cities,active FROM geo_dataset ORDER BY id DESC`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var d Dataset
		if err := rows.Scan(&d.ID, &d.Version, &d.Created, &d.Countries, &d.Regions, &d.Cities, &d.Active); err != nil {
			return result, err
		}
		result.Datasets = append(result.Datasets, d)
	}
	return result, rows.Err()
}
func (s *Store) Activate(ctx context.Context, id int64) error {
	if !s.importMu.TryLock() {
		return ErrBusy
	}
	defer s.importMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM geo_dataset WHERE id=?`, id).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return ErrInvalid
	}
	if _, err = tx.ExecContext(ctx, `UPDATE geo_dataset SET active=0 WHERE active=1`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE geo_dataset SET active=1 WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

const active = `(SELECT id FROM geo_dataset WHERE active=1)`

func (s *Store) Countries(ctx context.Context) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT code,name FROM geo_country WHERE dataset=`+active+` ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Item{}
	for rows.Next() {
		var i Item
		if err = rows.Scan(&i.ID, &i.Name); err != nil {
			return nil, err
		}
		result = append(result, i)
	}
	return result, rows.Err()
}
func (s *Store) Regions(ctx context.Context, country string) ([]Item, error) {
	if !validCountry(country) {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT code,name,ascii FROM geo_region WHERE dataset=`+active+` AND country=? ORDER BY name`, country)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Item{}
	for rows.Next() {
		i := Item{Country: country}
		if err = rows.Scan(&i.ID, &i.Name, &i.ASCII); err != nil {
			return nil, err
		}
		result = append(result, i)
	}
	return result, rows.Err()
}
func (s *Store) Cities(ctx context.Context, country, region, query string) ([]Item, error) {
	if !validCountry(country) || len(region) > 30 || len(query) > 400 {
		return nil, ErrInvalid
	}
	query = strings.ToLower(strings.TrimSpace(query))
	search := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	// A correlated region lookup keeps each response within a single DB snapshot
	// even when an administrator activates a new catalogue concurrently.
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.name,c.ascii,c.region,COALESCE((SELECT r.name FROM geo_region r WHERE r.dataset=c.dataset AND r.country=c.country AND r.code=c.region),'') FROM geo_city c WHERE c.dataset=`+active+` AND c.country=? AND (?='' OR c.region=?) AND (?='' OR c.search LIKE ? ESCAPE '\') ORDER BY c.population DESC,c.name,c.id LIMIT 30`, country, region, region, query, "%"+search+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Item{}
	for rows.Next() {
		i := Item{Country: country}
		if err = rows.Scan(&i.ID, &i.Name, &i.ASCII, &i.Region, &i.RegionName); err != nil {
			return nil, err
		}
		result = append(result, i)
	}
	return result, rows.Err()
}
func validCountry(code string) bool {
	return len(code) == 2 && code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z'
}
func dataError(kind string, line int) error {
	return fmt.Errorf("%w: %s 第 %d 行格式或关联无效", ErrInvalid, kind, line)
}
