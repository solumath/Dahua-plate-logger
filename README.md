# Dahua Plate Logger

Listens to TrafficJunction events from one or more Dahua cameras and logs detected licence plates to SQLite. Tested with ITC237-PW6M-IRLZF-C2.

## Output

| Path | Description |
|------|-------------|
| `data/plates/YYYY.db` | SQLite database, one file per year |
| `data/logs/<date>.log` | App log, daily rotation |
| `data/logs/raw/<date>.log` | Raw HTTP stream from all cameras, daily rotation |
| `data/exports/` | CSV files produced by `make export-*` |

## Configuration

### `cameras.yaml`

Place in `config/cameras.yaml` (or mount to `/config/cameras.yaml` in Docker):

```yaml
cameras:
  - name: entrance
    url: "http://192.168.1.10/cgi-bin/eventManager.cgi?action=attach&codes=[TrafficJunction]"
    user: admin
    pass: yourpassword
```

`cameras.yaml` is gitignored — keep credentials out of version control.

### Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_DIR` | `/data/plates` | SQLite directory |
| `LOG_DIR` | `/data/logs` | Log directory |
| `LOG_LEVEL` | `info` | `info` \| `debug` \| `all` |
| `PORT` | `8080` | Status server port; set to `0` to disable |

**Log levels:**
- `info` — valid plates only (1–8 uppercase alphanumeric characters)
- `debug` — + malformed/unrecognised plates
- `all` — + every other camera event (heartbeat, etc.)

## API

The status server exposes the following endpoints (default port 8080):

| Endpoint | Response | Description |
|----------|----------|-------------|
| `GET /` | HTML | Status page — connection state and last plate per camera |
| `GET /status` | JSON | Uptime and per-camera state (`camera`, `state`, `attempt`, `error`) |
| `GET /plates` | JSON | Last 100 detected plates |
| `GET /export?from=YYYY-MM-DD&to=YYYY-MM-DD` | CSV | Plate export for date range |

## Building

```bash
make build     # produces ./plate_logger
```

Requires Go. No CGO — the binary is fully static.

## Running

```bash
./plate_logger
```

The binary reads `/config/cameras.yaml` and reconnects automatically after connection drops.

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
