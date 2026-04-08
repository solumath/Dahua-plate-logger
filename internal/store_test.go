package logger

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir())
}

func mustInsert(t *testing.T, s *Store, plate, camera string, ts time.Time) {
	t.Helper()
	e := Event{UTC: ts.Unix(), UTCMS: ts.UnixMilli() % 1000, Plate: plate, Camera: camera}
	if err := s.Insert(e); err != nil {
		t.Fatalf("Insert: %v", err)
	}
}

// --- Store ---

func TestStore_InsertAndQuery(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	ts := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	mustInsert(t, s, "9P08278", "entrance", ts)

	var rows []PlateRow
	if err := s.QueryRange(time.Time{}, time.Time{}, func(r PlateRow) error {
		rows = append(rows, r)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Plate != "9P08278" || rows[0].Camera != "entrance" || rows[0].LocalDatetime == "" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestStore_YearSharding(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	mustInsert(t, s, "Y25", "cam", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	mustInsert(t, s, "Y26", "cam", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	if _, err := os.Stat(s.DBPath(2025)); err != nil {
		t.Fatal("expected 2025.db to exist")
	}
	if _, err := os.Stat(s.DBPath(2026)); err != nil {
		t.Fatal("expected 2026.db to exist")
	}
}

func TestStore_DateRangeFilter(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	for i, d := range []string{"2026-03-20", "2026-03-21", "2026-03-22"} {
		ts, _ := time.Parse("2006-01-02", d)
		mustInsert(t, s, d, "cam", ts.Add(time.Duration(i)*time.Hour))
	}

	from := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 21, 23, 59, 59, 0, time.UTC)

	var plates []string
	if err := s.QueryRange(from, to, func(r PlateRow) error {
		plates = append(plates, r.Plate)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(plates) != 1 || plates[0] != "2026-03-21" {
		t.Fatalf("want [2026-03-21], got %v", plates)
	}
}

// --- ExportCSV ---

func TestExportCSV(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	mustInsert(t, s, "9P08278", "entrance", time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC))
	mustInsert(t, s, "1AB2345", "exit", time.Date(2026, 3, 20, 11, 0, 0, 0, time.UTC))
	// out of range — should not appear
	mustInsert(t, s, "OUTRANGE", "cam", time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC))

	from := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 20, 23, 59, 59, 0, time.UTC)

	var buf bytes.Buffer
	if err := ExportCSV(s, from, to, &buf); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if lines[0] != "DateTime,PlateNumber,Camera" {
		t.Fatalf("bad header: %q", lines[0])
	}
	if len(lines) != 3 {
		t.Fatalf("want 3 lines (header+2), got %d:\n%s", len(lines), buf.String())
	}
	if !strings.Contains(lines[1], "9P08278") || !strings.Contains(lines[2], "1AB2345") {
		t.Fatalf("unexpected rows:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "OUTRANGE") {
		t.Fatal("out-of-range row appeared in export")
	}
}
