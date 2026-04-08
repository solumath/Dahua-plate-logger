package logger

import (
	"encoding/csv"
	"io"
	"os"
	"time"
)

// ExportCSV writes plate rows to w in CSV format.
// Pass zero time.Time values to dump all records.
func ExportCSV(store *Store, from, to time.Time, w io.Writer) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"DateTime", "PlateNumber", "Camera"}); err != nil {
		return err
	}
	err := store.QueryRange(from, to, func(row PlateRow) error {
		return cw.Write([]string{row.LocalDatetime, row.Plate, row.Camera})
	})
	if err != nil {
		return err
	}
	cw.Flush()
	return cw.Error()
}

// ExportCSVToFile is like ExportCSV but writes to a file at path.
func ExportCSVToFile(store *Store, from, to time.Time, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return ExportCSV(store, from, to, f)
}
