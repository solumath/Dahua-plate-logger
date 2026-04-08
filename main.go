package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	logger "github.com/solumath/dahua-plate-logger/internal"
	"gopkg.in/yaml.v3"
)

type config struct {
	DBDir    string
	LogDir   string
	LogLevel string
	Port     int
	Cameras  []logger.CameraConfig
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadConfig() (*config, error) {
	f, err := os.Open("/config/cameras.yaml")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cameras struct {
		Cameras []logger.CameraConfig `yaml:"cameras"`
	}
	if err := yaml.NewDecoder(f).Decode(&cameras); err != nil {
		return nil, err
	}

	port, _ := strconv.Atoi(getEnv("PORT", "8080"))

	return &config{
		DBDir:    getEnv("DB_DIR", "/data/plates"),
		LogDir:   getEnv("LOG_DIR", "/data/logs"),
		LogLevel: getEnv("LOG_LEVEL", "info"),
		Port:     port,
		Cameras:  cameras.Cameras,
	}, nil
}

func buildStreams(cameras []logger.CameraConfig, rawLog io.Writer) ([]*logger.CameraStream, error) {
	streams := make([]*logger.CameraStream, 0, len(cameras))
	for _, cam := range cameras {
		stream, err := logger.NewCameraStream(cam)
		if err != nil {
			return nil, err
		}
		stream.SetRawWriter(rawLog)
		streams = append(streams, stream)
	}
	return streams, nil
}

func run(ctx context.Context, streams []*logger.CameraStream, store *logger.Store, hs *logger.StatusServer) {
	var wg sync.WaitGroup
	for _, stream := range streams {
		wg.Add(1)
		go func(stream *logger.CameraStream) {
			defer wg.Done()
			var attempt int
			for {
				attempt++
				if hs != nil {
					hs.SetConnecting(stream.Name(), attempt)
				}
				if err := stream.Connect(ctx, store); err != nil {
					if ctx.Err() != nil {
						slog.Info("camera goroutine stopping", "camera", stream.Name())
						return
					}
					if hs != nil {
						hs.SetError(stream.Name(), err)
					}
					slog.Error("stream error", "camera", stream.Name(), "attempt", attempt, "err", err)
					select {
					case <-time.After(3 * time.Second):
					case <-ctx.Done():
						return
					}
				} else {
					attempt = 0 // reset so next reconnect counts from attempt 1
				}
			}
		}(stream)
	}
	wg.Wait()
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("failed to load cameras.yaml", "err", err)
		os.Exit(1)
	}
	if len(cfg.Cameras) == 0 {
		slog.Error("no cameras defined in cameras.yaml")
		os.Exit(1)
	}

	logger.SetupLogging(cfg.LogDir, logger.ParseLevel(cfg.LogLevel))
	rawLog := logger.NewRawLog(filepath.Join(cfg.LogDir, "raw"))

	streams, err := buildStreams(cfg.Cameras, rawLog)
	if err != nil {
		slog.Error("invalid camera config", "err", err)
		os.Exit(1)
	}

	store := logger.NewStore(cfg.DBDir)
	defer store.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var hs *logger.StatusServer
	if cfg.Port > 0 {
		hs = logger.NewStatusServer(streams, store)
		for _, stream := range streams {
			stream.SetOnEvent(hs.Touch)
			stream.SetOnConnect(func() { hs.SetConnected(stream.Name()) })
		}
		hs.Start(ctx, cfg.Port)
	}

	slog.Info("plate_logger starting", "cameras", len(streams), "db_dir", cfg.DBDir, "log_level", cfg.LogLevel)
	for _, stream := range streams {
		slog.Info("camera registered", "camera", stream.Name())
	}

	run(ctx, streams, store, hs)
	slog.Info("plate_logger stopped")
}
