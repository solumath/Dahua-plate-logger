# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make build          # CGO_ENABLED=0 go build -o plate_logger .
make run            # go run .
make test           # go test ./internal/...
go test ./internal/... -run TestName   # single test
make export-today   # export today's plates to CSV
```

## Architecture

The service streams HTTP multipart events from one or more Dahua cameras concurrently, parses plate detections, and persists them to SQLite.

**Data flow:** `CameraStream.Connect` opens a persistent HTTP connection → raw bytes tee'd to `logs/raw/<date>.log` → `iterLines` scans line-by-line → collects multi-line JSON on `Code=TrafficJunction;action=Pulse;index=0;data=` → `parseEvent` unmarshals to `Event{UTC, UTCMS, Plate, Camera}` → `Store.Insert` writes to SQLite.

**Structure:**

```
main.go              — orchestration only: loadConfig, buildStreams, run
cmd/export/          — standalone CLI for CSV export (-range today|week|month|year|all)
internal/
  logging.go         — LevelAll, plateRe/isValidPlate, dailyWriter, SetupLogging, ParseLevel, NewRawLog
  stream.go          — CameraConfig, CameraStream, Event (UTC/UTCMS primary; Time() derives local), iterLines, parseEvent, braceDepthDelta
  store.go           — Store: per-year SQLite sharding, Insert, QueryRange; minYear=2000 lower bound
  export.go          — ExportCSV, ExportCSVToFile
  stream_test.go     — stream parsing tests using real ITC237 wire format
  store_test.go      — store + export tests
```

**Configuration** (`cameras.yaml` next to the binary, gitignored):

```yaml
db_dir: plates       # optional, default: plates/ next to binary
log_dir: logs        # optional, default: logs/ next to binary
log_level: info      # optional: info | debug | all
port: 8080           # optional, default: 8080; set to 0 to disable status server

cameras:
  - name: entrance
    url: "http://.../eventManager.cgi?action=attach&codes=[TrafficJunction]"
    user: admin
    pass: password
```

**Log levels:**
- `info` — valid plates only (`^[A-Z0-9]{1,8}$`)
- `debug` — + malformed plates and no-plate events
- `all` — + every other camera event code (heartbeat, etc.)

**Logs written to `log_dir/`:**
- `<date>.log` — app log, daily rotation
- `raw/<date>.log` — full raw HTTP stream, one shared file across all cameras

**Timestamps:** The camera's `UTC`/`UTCMS` fields are true UTC seconds and milliseconds. `Event.Time()` converts them to local time via `time.UnixMilli(UTC*1000 + UTCMS).Local()`. `local_datetime` in SQLite is stored as a local-time string (`YYYY-MM-DD HH:MM:SS.mmm`). `utc`/`utc_ms` columns store the raw camera values for range queries.

**DB lifecycle:** SQLite files are created lazily on the first `Insert` for a given year — nothing is created at startup.
