package logger

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// minYear is the earliest year considered when no lower bound is given in a query.
// Dahua ITC cameras were not in use before this.
const minYear = 2000

const schema = `
CREATE TABLE IF NOT EXISTS plates (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	utc            INTEGER NOT NULL,
	utc_ms         INTEGER NOT NULL DEFAULT 0,
	local_datetime TEXT    NOT NULL,
	plate          TEXT    NOT NULL,
	camera         TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plates_utc    ON plates(utc, utc_ms);
CREATE INDEX IF NOT EXISTS idx_plates_camera ON plates(camera);
`

// Store manages per-year SQLite databases under a single directory.
type Store struct {
	mu  sync.RWMutex
	dir string
	dbs map[int]*sql.DB
}

func NewStore(dir string) *Store {
	return &Store{dir: dir, dbs: make(map[int]*sql.DB)}
}

func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, db := range s.dbs {
		db.Close()
	}
}

func (s *Store) DBPath(year int) string {
	return filepath.Join(s.dir, fmt.Sprintf("%d.db", year))
}

func (s *Store) openDB(year int) (*sql.DB, error) {
	db, err := sql.Open("sqlite", s.DBPath(year))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s.dbs[year] = db
	return db, nil
}

func (s *Store) getDB(year int) (*sql.DB, error) {
	if db, ok := s.dbs[year]; ok {
		return db, nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, err
	}
	db, err := s.openDB(year)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		delete(s.dbs, year)
		return nil, err
	}
	return db, nil
}

func (s *Store) getDBReadOnly(year int) (*sql.DB, error) {
	if db, ok := s.dbs[year]; ok {
		return db, nil
	}
	if _, err := os.Stat(s.DBPath(year)); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return s.openDB(year)
}

func (s *Store) Insert(e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := e.Time()
	db, err := s.getDB(t.Year())
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`INSERT INTO plates (utc, utc_ms, local_datetime, plate, camera) VALUES (?, ?, ?, ?, ?)`,
		e.UTC,
		e.UTCMS,
		t.Format("2006-01-02 15:04:05.000"),
		e.Plate,
		e.Camera,
	)
	return err
}

// PlateRow is a row returned from a query.
type PlateRow struct {
	UTC           int64  `json:"utc"`
	UTCMS         int64  `json:"utc_ms"`
	LocalDatetime string `json:"datetime"`
	Plate         string `json:"plate"`
	Camera        string `json:"camera"`
}

// QueryRange streams rows in [from, to] order, calling fn for each.
// Zero time.Time values mean no bound.
func (s *Store) QueryRange(from, to time.Time, fn func(PlateRow) error) error {
	years, err := s.availableYears(from, to)
	if err != nil {
		return err
	}

	var fromUTC, toUTC int64
	if !from.IsZero() {
		fromUTC = from.Unix()
	}
	if !to.IsZero() {
		toUTC = to.Unix()
	}

	for _, year := range years {
		s.mu.Lock()
		db, err := s.getDBReadOnly(year)
		s.mu.Unlock()
		if err != nil {
			return fmt.Errorf("open year %d: %w", year, err)
		}
		if db == nil {
			continue
		}

		var conds []string
		var args []any
		if fromUTC != 0 {
			conds = append(conds, "utc >= ?")
			args = append(args, fromUTC)
		}
		if toUTC != 0 {
			conds = append(conds, "utc <= ?")
			args = append(args, toUTC)
		}
		q := `SELECT utc, utc_ms, local_datetime, plate, camera FROM plates`
		if len(conds) > 0 {
			q += ` WHERE ` + strings.Join(conds, ` AND `)
		}
		q += ` ORDER BY utc, utc_ms`

		if err := func() error {
			rows, err := db.Query(q, args...)
			if err != nil {
				return fmt.Errorf("query year %d: %w", year, err)
			}
			defer rows.Close()
			for rows.Next() {
				var row PlateRow
				if err := rows.Scan(&row.UTC, &row.UTCMS, &row.LocalDatetime, &row.Plate, &row.Camera); err != nil {
					return fmt.Errorf("scan year %d: %w", year, err)
				}
				if err := fn(row); err != nil {
					return err
				}
			}
			return rows.Err()
		}(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) availableYears(from, to time.Time) ([]int, error) {
	if !from.IsZero() || !to.IsZero() {
		start := from.Year()
		if from.IsZero() {
			start = minYear
		}
		end := to.Year()
		if to.IsZero() {
			end = time.Now().Year()
		}
		years := make([]int, 0, end-start+1)
		for y := start; y <= end; y++ {
			years = append(years, y)
		}
		return years, nil
	}

	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var years []int
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".db") {
			continue
		}
		if y, err := strconv.Atoi(strings.TrimSuffix(name, ".db")); err == nil {
			years = append(years, y)
		}
	}
	sort.Ints(years)
	return years, nil
}

// QueryLatestPerCamera returns the most recent plate for each named camera.
// Checks current year first, spills into previous year if a camera has no rows yet.
func (s *Store) QueryLatestPerCamera(cameras []string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make(map[string]string, len(cameras))
	for _, year := range []int{time.Now().Year(), time.Now().Year() - 1} {
		db, err := s.getDBReadOnly(year)
		if err != nil {
			return nil, err
		}
		if db == nil {
			continue
		}
		for _, cam := range cameras {
			if _, found := result[cam]; found {
				continue
			}
			var plate string
			err := db.QueryRow(
				`SELECT plate FROM plates WHERE camera = ? ORDER BY utc DESC, utc_ms DESC LIMIT 1`,
				cam,
			).Scan(&plate)
			if err == sql.ErrNoRows {
				continue
			}
			if err != nil {
				return nil, err
			}
			result[cam] = plate
		}
		if len(result) == len(cameras) {
			break
		}
	}
	return result, nil
}

// QueryRecent returns up to n plates ordered most-recent-first.
// Queries the current year first; spills into the previous year if needed.
func (s *Store) QueryRecent(n int) ([]PlateRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var rows []PlateRow
	for _, year := range []int{time.Now().Year(), time.Now().Year() - 1} {
		db, err := s.getDBReadOnly(year)
		if err != nil {
			return nil, err
		}
		if db == nil {
			continue
		}
		err = func() error {
			r, err := db.Query(
				`SELECT utc, utc_ms, local_datetime, plate, camera FROM plates ORDER BY utc DESC, utc_ms DESC LIMIT ?`,
				n-len(rows),
			)
			if err != nil {
				return err
			}
			defer r.Close()
			for r.Next() {
				var row PlateRow
				if err := r.Scan(&row.UTC, &row.UTCMS, &row.LocalDatetime, &row.Plate, &row.Camera); err != nil {
					return err
				}
				rows = append(rows, row)
			}
			return r.Err()
		}()
		if err != nil {
			return nil, err
		}
		if len(rows) >= n {
			break
		}
	}
	return rows, nil
}
