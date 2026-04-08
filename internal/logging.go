package logger

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// LevelAll is below Debug; enables logging of all camera events regardless of type.
const LevelAll = slog.Level(-8)

// plateRe matches licence plates: 1–8 uppercase alphanumeric characters.
var plateRe = regexp.MustCompile(`^[A-Z0-9]{1,8}$`)

func isValidPlate(p string) bool { return plateRe.MatchString(p) }

// dailyWriter writes to <dir>/<YYYY-MM-DD>.log, rotating at midnight.
type dailyWriter struct {
	mu   sync.Mutex
	dir  string
	date string
	f    *os.File
}

func newDailyWriter(dir string) *dailyWriter { return &dailyWriter{dir: dir} }

func (w *dailyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	if w.date != today {
		if w.f != nil {
			w.f.Close()
			w.f = nil
		}
		if err := os.MkdirAll(w.dir, 0o755); err != nil {
			return 0, err
		}
		f, err := os.OpenFile(filepath.Join(w.dir, today+".log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return 0, err
		}
		w.f = f
		w.date = today
	}
	return w.f.Write(p)
}

// NewRawLog returns a daily-rotating writer for raw camera stream bytes.
func NewRawLog(dir string) io.Writer { return newDailyWriter(dir) }

// SetupLogging configures the default slog handler to write to stdout and a
// daily rotating file in logDir.
func SetupLogging(logDir string, level slog.Level) {
	w := newDailyWriter(logDir)
	h := slog.NewTextHandler(io.MultiWriter(os.Stdout, w), &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				if l, ok := a.Value.Any().(slog.Level); ok && l == LevelAll {
					a.Value = slog.StringValue("ALL")
				}
			}
			return a
		},
	})
	slog.SetDefault(slog.New(h))
}

// ParseLevel parses a log level string, extending slog with "all".
func ParseLevel(s string) slog.Level {
	if strings.ToLower(s) == "all" {
		return LevelAll
	}
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return l
}
