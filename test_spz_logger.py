import csv
import json
import logging
import os
from datetime import datetime, timezone
from unittest.mock import MagicMock, patch

import pytest

os.environ.setdefault("CAMERA_URL", "http://test-camera/events")
os.environ.setdefault("CAMERA_USER", "admin")
os.environ.setdefault("CAMERA_PASS", "password")

import spz_logger


@pytest.fixture(autouse=True)
def remove_file_handlers():
    logger = logging.getLogger("spz_logger")
    for h in [h for h in logger.handlers if isinstance(h, logging.FileHandler)]:
        logger.removeHandler(h)
        h.close()


# ---------------------------------------------------------------------------
# utc_ms_to_local
# ---------------------------------------------------------------------------

def test_utc_ms_to_local_epoch():
    assert spz_logger.utc_ms_to_local(0, 0) == "1970-01-01 00:00:00.000"


def test_utc_ms_to_local_with_milliseconds():
    result = spz_logger.utc_ms_to_local(1700000000, 500)
    expected = datetime.fromtimestamp(1700000000.5, timezone.utc).strftime("%Y-%m-%d %H:%M:%S.%f")[:-3]
    assert result == expected


def test_utc_ms_to_local_no_ms_defaults_to_zero():
    result = spz_logger.utc_ms_to_local(1700000000)
    assert result.endswith(".000")


# ---------------------------------------------------------------------------
# csv_path_for
# ---------------------------------------------------------------------------

def test_csv_path_for(tmp_path, monkeypatch):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    assert spz_logger.csv_path_for("2026-03-20 10:30:00.000") == tmp_path / "2026-03-20.csv"


def test_csv_path_for_uses_only_date_portion(tmp_path, monkeypatch):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    assert spz_logger.csv_path_for("2026-03-20 23:59:59.999") == tmp_path / "2026-03-20.csv"


# ---------------------------------------------------------------------------
# append_row
# ---------------------------------------------------------------------------

def test_append_row_creates_file_with_header(tmp_path, monkeypatch):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    spz_logger.append_row({"DateTime": "2026-03-20 10:00:00.000", "PlateNumber": "ABC123"})
    rows = list(csv.DictReader(open(tmp_path / "2026-03-20.csv")))
    assert rows[0] == {"DateTime": "2026-03-20 10:00:00.000", "PlateNumber": "ABC123"}


def test_append_row_no_duplicate_header(tmp_path, monkeypatch):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    spz_logger.append_row({"DateTime": "2026-03-20 10:00:00.000", "PlateNumber": "AAA111"})
    spz_logger.append_row({"DateTime": "2026-03-20 11:00:00.000", "PlateNumber": "BBB222"})
    rows = list(csv.DictReader(open(tmp_path / "2026-03-20.csv")))
    assert len(rows) == 2


def test_append_row_separate_files_per_day(tmp_path, monkeypatch):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    spz_logger.append_row({"DateTime": "2026-03-20 10:00:00.000", "PlateNumber": "A1"})
    spz_logger.append_row({"DateTime": "2026-03-21 10:00:00.000", "PlateNumber": "B2"})
    assert (tmp_path / "2026-03-20.csv").exists()
    assert (tmp_path / "2026-03-21.csv").exists()


# ---------------------------------------------------------------------------
# log_raw
# ---------------------------------------------------------------------------

def test_log_raw_writes_jsonl(tmp_path, monkeypatch):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    event = {"UTC": 1000, "PlateNumber": "TEST123"}
    spz_logger.log_raw(event)
    today = datetime.now().strftime("%Y-%m-%d")
    lines = (tmp_path / f"{today}.jsonl").read_text().splitlines()
    assert json.loads(lines[0]) == event


def test_log_raw_appends_multiple_events(tmp_path, monkeypatch):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    spz_logger.log_raw({"id": 1})
    spz_logger.log_raw({"id": 2})
    today = datetime.now().strftime("%Y-%m-%d")
    lines = (tmp_path / f"{today}.jsonl").read_text().splitlines()
    assert len(lines) == 2
    assert json.loads(lines[1])["id"] == 2


# ---------------------------------------------------------------------------
# parse_event
# ---------------------------------------------------------------------------

def _make_event(utc=1700000000, utcms=0, obj_type="Plate", obj_text="1A23456", tc_plate=None):
    return {
        "UTC": utc,
        "UTCMS": utcms,
        "Object": {"ObjectType": obj_type, "Text": obj_text},
        "TrafficCar": {"PlateNumber": tc_plate} if tc_plate else {},
    }


def test_parse_event_plate_from_object(tmp_path, monkeypatch):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    spz_logger.parse_event(json.dumps(_make_event(obj_type="Plate", obj_text="1A23456")))
    rows = list(csv.DictReader(open(next(tmp_path.glob("*.csv")))))
    assert rows[0]["PlateNumber"] == "1A23456"


def test_parse_event_plate_from_traffic_car(tmp_path, monkeypatch):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    event = _make_event(obj_type="Vehicle", obj_text="Toyota", tc_plate="5B67890")
    spz_logger.parse_event(json.dumps(event))
    rows = list(csv.DictReader(open(next(tmp_path.glob("*.csv")))))
    assert rows[0]["PlateNumber"] == "5B67890"


def test_parse_event_object_plate_takes_priority_over_traffic_car(tmp_path, monkeypatch):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    event = _make_event(obj_type="Plate", obj_text="OBJECT1", tc_plate="TRAFFIC1")
    spz_logger.parse_event(json.dumps(event))
    rows = list(csv.DictReader(open(next(tmp_path.glob("*.csv")))))
    assert rows[0]["PlateNumber"] == "OBJECT1"


def test_parse_event_no_plate_writes_no_csv(tmp_path, monkeypatch):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    event = {"UTC": 1700000000, "Object": {}, "TrafficCar": {}}
    spz_logger.parse_event(json.dumps(event))
    assert list(tmp_path.glob("*.csv")) == []


def test_parse_event_invalid_json_writes_no_csv(tmp_path, monkeypatch, caplog):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    with caplog.at_level(logging.WARNING, logger="spz_logger"):
        spz_logger.parse_event("not valid json {{{")
    assert list(tmp_path.glob("*.csv")) == []
    assert any("JSON parse error" in r.message for r in caplog.records)


def test_parse_event_datetime_from_utc(tmp_path, monkeypatch):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    spz_logger.parse_event(json.dumps(_make_event(utc=1700000000, utcms=0)))
    rows = list(csv.DictReader(open(next(tmp_path.glob("*.csv")))))
    expected_dt = spz_logger.utc_ms_to_local(1700000000, 0)
    assert rows[0]["DateTime"] == expected_dt


# ---------------------------------------------------------------------------
# _DailyRotatingFileHandler
# ---------------------------------------------------------------------------

def test_rotation_filename_uses_logs_subdir(tmp_path):
    log_file = tmp_path / "spz_logger.log"
    handler = spz_logger._DailyRotatingFileHandler(log_file, backupCount=30)
    handler.close()
    result = handler.rotation_filename(str(log_file) + ".2026-03-19")
    assert result == str(tmp_path / "logs" / "2026-03-19.log")


# ---------------------------------------------------------------------------
# popup
# ---------------------------------------------------------------------------

def test_popup_does_nothing_on_non_windows(monkeypatch):
    import ctypes
    monkeypatch.delattr(ctypes, "windll", raising=False)
    # Should return without error on non-Windows
    spz_logger.popup("Title", "Message")
    spz_logger.popup("Title", "Error", error=True)


# ---------------------------------------------------------------------------
# stream_and_process
# ---------------------------------------------------------------------------

def _make_mock_response(lines: list[str]):
    mock_resp = MagicMock()
    mock_resp.status_code = 200
    mock_resp.raise_for_status = MagicMock()
    mock_resp.iter_lines.return_value = iter(lines)
    mock_resp.__enter__ = lambda s: s
    mock_resp.__exit__ = MagicMock(return_value=False)
    return mock_resp


def test_stream_parses_plate_event(tmp_path, monkeypatch):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    event = _make_event(obj_type="Plate", obj_text="STREAM1")
    event_lines = json.dumps(event, indent=2).splitlines()
    # Camera sends opening brace on the Code= line, rest on subsequent lines
    lines = [f"Code=TrafficJunction;action=Pulse;data={event_lines[0]}"] + event_lines[1:]
    with patch("spz_logger.requests.get", return_value=_make_mock_response(lines)):
        spz_logger.stream_and_process()
    rows = list(csv.DictReader(open(next(tmp_path.glob("*.csv")))))
    assert rows[0]["PlateNumber"] == "STREAM1"


def test_stream_parses_multiline_json(tmp_path, monkeypatch):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    event = _make_event(obj_type="Plate", obj_text="MULTI1")
    event_str = json.dumps(event, indent=2)
    first_line, *rest_lines = event_str.splitlines()
    lines = [f"Code=TrafficJunction;action=Pulse;data={first_line}"] + rest_lines
    with patch("spz_logger.requests.get", return_value=_make_mock_response(lines)):
        spz_logger.stream_and_process()
    rows = list(csv.DictReader(open(next(tmp_path.glob("*.csv")))))
    assert rows[0]["PlateNumber"] == "MULTI1"


def test_stream_ignores_non_traffic_lines(tmp_path, monkeypatch):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    lines = ["SomeOtherEvent;action=Pulse;data={}", "boundary", "--myboundary"]
    with patch("spz_logger.requests.get", return_value=_make_mock_response(lines)):
        spz_logger.stream_and_process()
    assert list(tmp_path.glob("*.csv")) == []


def test_stream_warns_on_missing_data_field(tmp_path, monkeypatch, caplog):
    monkeypatch.setattr(spz_logger, "OUTPUT_DIR", tmp_path)
    lines = ["Code=TrafficJunction;action=Pulse;info=here"]
    with patch("spz_logger.requests.get", return_value=_make_mock_response(lines)):
        with caplog.at_level(logging.WARNING, logger="spz_logger"):
            spz_logger.stream_and_process()
    assert any("No 'data='" in r.message for r in caplog.records)


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------

def test_main_exits_if_already_running(monkeypatch):
    mock_socket = MagicMock()
    mock_socket.bind.side_effect = OSError("address in use")
    monkeypatch.setattr(spz_logger, "_lock_socket", mock_socket)
    with pytest.raises(SystemExit) as exc:
        spz_logger.main()
    assert exc.value.code == 1


def test_main_reconnects_on_exception(monkeypatch):
    mock_socket = MagicMock()
    mock_socket.bind = MagicMock()
    monkeypatch.setattr(spz_logger, "_lock_socket", mock_socket)
    monkeypatch.setattr(spz_logger, "popup", MagicMock())

    call_count = 0

    def fake_stream():
        nonlocal call_count
        call_count += 1
        if call_count == 1:
            raise ConnectionError("timeout")
        raise KeyboardInterrupt

    monkeypatch.setattr(spz_logger, "stream_and_process", fake_stream)
    monkeypatch.setattr(spz_logger.time, "sleep", MagicMock())

    with pytest.raises(SystemExit):
        spz_logger.main()

    assert call_count == 2
