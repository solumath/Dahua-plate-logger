# Dahua Plate Logger

Listens to TrafficJunction events from one or more Dahua cameras and logs detected licence plates to SQLite. Tested with ITC237-PW6M-IRLZF-C2.

## Output

| Path | Description |
|------|-------------|
| `plates/YYYY.db` | SQLite database, one file per year |
| `logs/<date>.log` | App log, daily rotation |
| `logs/raw/<date>.log` | Raw HTTP stream from all cameras, daily rotation |
| `exports/` | CSV files produced by `make export-*` |

## Configuration

Copy `cameras.example.yaml` to `cameras.yaml` next to the binary and edit it:

```yaml
db_dir: plates       # optional
log_dir: logs        # optional
log_level: info      # info | debug | all
port: 8080           # optional; set to 0 to disable the status server

cameras:
  - name: entrance
    url: "http://192.168.1.10/cgi-bin/eventManager.cgi?action=attach&codes=[TrafficJunction]"
    user: admin
    pass: yourpassword
```

`cameras.yaml` is gitignored — keep credentials out of version control.

**Log levels:**
- `info` — valid plates only (1–8 uppercase alphanumeric characters)
- `debug` — + malformed/unrecognised plates
- `all` — + every other camera event (heartbeat, etc.)

## Building

```bash
make build     # produces ./plate_logger
```

Requires Go. No CGO — the binary is fully static.

## Running

```bash
./plate_logger
```

The binary looks for `cameras.yaml` in the working directory, then next to the executable. It reconnects automatically after connection drops.

A status page is available at `http://localhost:<port>/` (default 8080) showing connection state and last plate per camera, with a date-range CSV export form.

## Exporting

```bash
make export-today
make export-week
make export-month
make export-year
make export-all
```

CSV columns: `DateTime`, `PlateNumber`, `Camera`.

## Deployment on Windows

1. Build on Linux/Mac with `GOOS=windows make build` or build directly on Windows with `make build`
2. Place `plate_logger.exe` and `cameras.yaml` in the same folder
3. Schedule via Task Scheduler: trigger *At startup*, action *Start a program*, restart on failure
