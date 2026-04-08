package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	logger "github.com/solumath/dahua-plate-logger/internal"
)

func main() {
	rangeFlag := flag.String("range", "all", "time range: today, week, month, year, all")
	dbDir := flag.String("db", "plates", "DB directory")
	outDir := flag.String("out", "exports", "output directory")
	flag.Parse()

	from, to, label := parseRange(*rangeFlag)

	store := logger.NewStore(*dbDir)
	defer store.Close()

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		os.Exit(1)
	}

	out := filepath.Join(*outDir, fmt.Sprintf("plates_%s.csv", label))
	if err := logger.ExportCSVToFile(store, from, to, out); err != nil {
		fmt.Fprintf(os.Stderr, "export failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("exported to %s\n", out)
}

func parseRange(r string) (from, to time.Time, label string) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	eod := today.Add(24*time.Hour - time.Millisecond)

	switch r {
	case "today":
		return today, eod, now.Format("2006-01-02")
	case "week":
		wd := int(now.Weekday())
		if wd == 0 {
			wd = 7 // Sunday → 7 so Monday is always day 1
		}
		monday := today.AddDate(0, 0, -(wd - 1))
		yr, wk := now.ISOWeek()
		return monday, eod, fmt.Sprintf("%d-W%02d", yr, wk)
	case "month":
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return start, eod, now.Format("2006-01")
	case "year":
		start := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
		return start, eod, fmt.Sprintf("%d", now.Year())
	default: // all
		return time.Time{}, time.Time{}, "all"
	}
}
