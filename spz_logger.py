#!/usr/bin/env python3
"""
author: Daniel Fajmon
email: dfajmon@centrum.cz

Camera event listener for car plates logging.
"""

import ctypes
import csv
import json
import logging
import logging.handlers
import socket
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

import os

import requests
from requests.auth import HTTPDigestAuth
from dotenv import load_dotenv


# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

SCRIPT_DIR = Path(sys.executable if getattr(sys, "frozen", False) else __file__).resolve().parent
load_dotenv(SCRIPT_DIR / ".env")

CAMERA_URL  = os.environ["CAMERA_URL"]
CAMERA_USER = os.environ["CAMERA_USER"]
CAMERA_PASS = os.environ["CAMERA_PASS"]
OUTPUT_DIR  = Path(os.getenv("OUTPUT_DIR") or SCRIPT_DIR / "plates")
LOG_FILE    = Path(os.getenv("LOG_FILE")   or SCRIPT_DIR / "spz_logger.log")


# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------

def setup_logging() -> logging.Logger:
    LOG_FILE.parent.mkdir(parents=True, exist_ok=True)
    logger = logging.getLogger("spz_logger")
    logger.setLevel(logging.DEBUG)

    fmt = logging.Formatter("%(asctime)s [%(levelname)s] %(message)s",
                            datefmt="%Y-%m-%d %H:%M:%S")

    # Rotating file — 5 MB per file, keep 3 backups
    fh = logging.handlers.RotatingFileHandler(
        LOG_FILE, maxBytes=5 * 1024 * 1024, backupCount=3, encoding="utf-8"
    )
    fh.setLevel(logging.DEBUG)
    fh.setFormatter(fmt)

    # Also print to stdout so manual runs show output in the terminal
    sh = logging.StreamHandler(sys.stdout)
    sh.setLevel(logging.INFO)
    sh.setFormatter(fmt)

    logger.addHandler(fh)
    logger.addHandler(sh)
    return logger


log = setup_logging()


def utc_ms_to_local(utc_sec: int, utc_ms: int = 0) -> str:
    """Format Dahua UTC + UTCMS fields as a datetime string with milliseconds."""
    ts = utc_sec + utc_ms / 1000.0
    dt = datetime.fromtimestamp(ts, timezone.utc)
    return dt.strftime("%Y-%m-%d %H:%M:%S.%f")[:-3]   # trim to milliseconds


def csv_path_for(dt_str: str) -> Path:
    """Return the CSV path for the date embedded in a datetime string."""
    date = dt_str[:10]          # 'YYYY-MM-DD'
    return OUTPUT_DIR / f"{date}.csv"


def append_row(row: dict) -> None:
    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
    path = csv_path_for(row["DateTime"])
    is_new = not path.exists()

    with open(path, "a", newline="") as fh:
        writer = csv.DictWriter(fh, fieldnames=["DateTime", "PlateNumber"])
        if is_new:
            writer.writeheader()
        writer.writerow(row)

    log.info("%s  %s  %s", path.name, row["DateTime"], row["PlateNumber"])


# ---------------------------------------------------------------------------
# Event parsing
# ---------------------------------------------------------------------------

def log_raw(event: dict) -> None:
    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
    date = datetime.now().strftime("%Y-%m-%d")
    path = OUTPUT_DIR / f"{date}.jsonl"
    with open(path, "a", encoding="utf-8") as fh:
        fh.write(json.dumps(event, ensure_ascii=False) + "\n")


def parse_event(json_str: str) -> None:
    """Parse a complete JSON event string and write a CSV row if a plate is present."""
    try:
        event = json.loads(json_str)
    except json.JSONDecodeError as exc:
        log.warning("JSON parse error: %s  raw=%s", exc, json_str[:200])
        return

    log_raw(event)

    tc  = event.get("TrafficCar", {})
    obj = event.get("Object", {})

    # Object.Text is only a plate when ObjectType == "Plate"; otherwise it's the vehicle brand.
    # Fall back to TrafficCar.PlateNumber for events where the plate object isn't primary.
    plate_text = obj.get("Text") if obj.get("ObjectType") == "Plate" else None
    plate = plate_text or tc.get("PlateNumber")
    if not plate:
        log.debug("Event with no plate detected, skipping (ObjectID=%s)", event.get("ObjectID"))
        return

    row = {
        "DateTime":    utc_ms_to_local(event.get("UTC", 0), event.get("UTCMS", 0)),
        "PlateNumber": plate,
    }
    append_row(row)


# ---------------------------------------------------------------------------
# Streaming
# ---------------------------------------------------------------------------

def stream_and_process() -> None:
    log.info("Connecting to %s", CAMERA_URL)
    with requests.get(
        CAMERA_URL,
        auth=HTTPDigestAuth(CAMERA_USER, CAMERA_PASS),
        stream=True,
        timeout=None,
    ) as resp:
        resp.raise_for_status()
        log.info("Connected — HTTP %s", resp.status_code)

        # The camera sends pretty-printed (multi-line) JSON after the Code= header.
        # We track brace depth to know when the JSON object is complete.
        collecting = False
        brace_depth = 0
        json_lines: list[str] = []

        for raw in resp.iter_lines(chunk_size=1):
            line: str = raw.decode("utf-8", errors="replace") if isinstance(raw, bytes) else raw

            if collecting:
                json_lines.append(line)
                brace_depth += line.count("{") - line.count("}")
                if brace_depth <= 0:
                    parse_event("\n".join(json_lines))
                    collecting = False
                    json_lines = []
                    brace_depth = 0
            elif line.startswith("Code=TrafficJunction;action=Pulse"):
                idx = line.find("data=")
                if idx == -1:
                    log.warning("No 'data=' in Code line: %s", line[:120])
                    continue
                first = line[idx + len("data="):]
                json_lines = [first]
                brace_depth = first.count("{") - first.count("}")
                collecting = brace_depth > 0  # start collecting if JSON opened


# ---------------------------------------------------------------------------
# Main reconnect loop
# ---------------------------------------------------------------------------

_lock_socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)


def popup(title: str, message: str, error: bool = False) -> None:
    if not hasattr(ctypes, "windll"):
        return
    icon = 0x10 if error else 0x40  # MB_ICONERROR or MB_ICONINFORMATION
    ctypes.windll.user32.MessageBoxW(0, message, title, icon)


def main() -> None:
    try:
        _lock_socket.bind(("127.0.0.1", 47892))
    except OSError:
        log.error("Another instance is already running — exiting")
        popup("Plate Logger", "Another instance is already running.", error=True)
        sys.exit(1)

    popup("Plate Logger", "Plate logger started successfully.")
    log.info("spz_logger starting")
    while True:
        try:
            stream_and_process()
        except KeyboardInterrupt:
            log.info("Interrupted by user — exiting")
            sys.exit(0)
        except Exception as exc:
            log.exception("Stream error: %s — reconnecting in 10 s", exc)
            time.sleep(10)


if __name__ == "__main__":
    main()
