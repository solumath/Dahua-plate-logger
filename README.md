# SPZ Logger

Dahua camera event listener. Listens to events on dahua camera and logs them to daily CSV files.
Specifically for ITC237-PW6M-IRLZF-C2, but should work with any Dahua camera that supports the same event API.

## Output

All output is written to the `plates/` folder next to the executable:

| File | Description |
| ---- | ----------- |
| `YYYY-MM-DD.csv` | Plate number and timestamp, one row per detection |
| `YYYY-MM-DD.jsonl` | Raw JSON events from the camera, one per line |
| `spz_logger.log` | Application log with rotating backup (10 MB × 3) |

## Configuration

Create a `.env` file next to the executable:

```env
CAMERA_URL=http://192.168.x.x/cgi-bin/eventManager.cgi?action=attach&codes=[TrafficJunction]
CAMERA_USER=admin
CAMERA_PASS=yourpassword
OUTPUT_DIR=   # optional, defaults to ./plates/
LOG_FILE=     # optional, defaults to ./spz_logger.log rotated files go to ./logs/
```

## Deployment on Windows

### 1. Install dependencies

```
pip install -r requirements.txt
pip install pyinstaller
```

### 2. Build the executable

```powershell
pyinstaller --onefile --noconsole --name spz_logger spz_logger.py
```

The executable will be at `dist\spz_logger.exe`.

### 3. Prepare the deployment folder

Copy these files into one folder:

```
spz_logger.exe
.env
stop.vbs
```

### 4. Autostart on boot

1. Open **Task Scheduler**
2. Click **Create Task**
3. **General** tab: check *Run only when user is logged on*
4. **Triggers** tab → New → *At startup*
5. **Actions** tab → New → *Start a program* → select `spz_logger.exe`
6. **Settings** tab: check *Restart the task if it fails*, set retry delay to 1 minute

### 5. Stopping the application

Double-click `stop.vbs` — it will find the running process and kill it, then show a confirmation popup.

## Running manually

**Using the executable:**

```
spz_logger.exe
```

**Using Python directly:**

```
python spz_logger.py
```

The script reconnects automatically after a connection drop.
