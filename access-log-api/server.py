import json
import os
import re
import socket
import socketserver
from collections import deque
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler
from urllib.parse import parse_qs, urlparse


PORT = int(os.getenv("PORT", "8090"))
ACCESS_LOG_DIR = os.getenv("ACCESS_LOG_DIR", "/var/log/trino-access")
SERVICE_NAME = os.getenv("SERVICE_NAME", "frontend")
CONTAINER_NAME = os.getenv("CONTAINER_NAME", "trino-search-frontend")
HOST_NAME = os.getenv("HOST_NAME", socket.gethostname())
TAIL_DEFAULT = 200
TAIL_MAX = 1000
JST = timezone(timedelta(hours=9))
DATE_RE = re.compile(r"^\d{4}-\d{2}-\d{2}$")


def today_jst():
    return datetime.now(JST).date().isoformat()


def parse_date(value):
    if value in (None, ""):
        return today_jst()
    if not DATE_RE.match(value):
        raise ValueError("invalid date")
    datetime.strptime(value, "%Y-%m-%d")
    return value


def parse_tail(value):
    if value is None or not value.isdigit():
        return TAIL_DEFAULT
    return min(max(int(value), 1), TAIL_MAX)


def read_lines(date, tail):
    path = os.path.join(ACCESS_LOG_DIR, f"access-{date}.jsonl")
    try:
        with open(path, encoding="utf-8") as handle:
            if tail is None:
                return [line.rstrip("\n") for line in handle]
            return list(deque((line.rstrip("\n") for line in handle), maxlen=tail))
    except FileNotFoundError:
        return []


def parse_entry(line):
    try:
        return json.loads(line)
    except (TypeError, ValueError):
        return None


def enrich(entry):
    entry = dict(entry)
    timestamp = entry.pop("time", None)
    return {
        "@timestamp": timestamp,
        "dt": timestamp[:10] if timestamp else None,
        "service": SERVICE_NAME,
        "host": HOST_NAME,
        "container": CONTAINER_NAME,
        **entry,
    }


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        parsed = urlparse(self.path)
        if parsed.path == "/health":
            self.send_json(200, {"status": "ok"})
            return
        if parsed.path != "/logs":
            self.send_json(404, {"error": "not found"})
            return

        query = parse_qs(parsed.query)
        try:
            date = parse_date((query.get("date") or [None])[0])
        except ValueError:
            self.send_json(400, {"error": "invalid date, expected YYYY-MM-DD"})
            return

        full = (query.get("full") or [""])[0] == "1"
        tail = None if full else parse_tail((query.get("tail") or [None])[0])
        logs = []
        for line in read_lines(date, tail):
            entry = parse_entry(line)
            if entry is not None and str(entry.get("time", "")).startswith(date):
                logs.append(enrich(entry))

        self.send_json(
            200,
            {
                "status": "ok",
                "service": SERVICE_NAME,
                "host": HOST_NAME,
                "container": CONTAINER_NAME,
                "date": date,
                "count": len(logs),
                "logs": logs,
            },
        )

    def send_json(self, status, payload):
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format_, *args):
        print(format_ % args, flush=True)


class Server(socketserver.ThreadingMixIn, socketserver.TCPServer):
    allow_reuse_address = True
    daemon_threads = True


if __name__ == "__main__":
    with Server(("0.0.0.0", PORT), Handler) as server:
        server.serve_forever()
