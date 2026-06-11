import os
import time
from datetime import date, datetime, time as datetime_time, timedelta, timezone
from urllib.parse import urljoin

import requests
from flask import Flask, jsonify, redirect, render_template, request, session, url_for


TRINO_URL = os.getenv("TRINO_URL", "http://trino1:8080")
TRINO_USER = os.getenv("TRINO_USER", "log_search")
TRINO_PASSWORD = os.getenv("TRINO_PASSWORD", "")
TRINO_CATALOG = os.getenv("TRINO_CATALOG", "iceberg")
TRINO_SCHEMA = os.getenv("TRINO_SCHEMA", "logs")
TRINO_SYSLOG_TABLE = os.getenv("TRINO_SYSLOG_TABLE", "syslog_events")
TRINO_AUTHLOG_TABLE = os.getenv("TRINO_AUTHLOG_TABLE", "authlog_events")
TRINO_TIMESTAMP_COLUMN = os.getenv("TRINO_TIMESTAMP_COLUMN", "ts")
TRINO_TIMESTAMP_EXPRESSION = os.getenv("TRINO_TIMESTAMP_EXPRESSION", "")
DEFAULT_LIMIT = int(os.getenv("TRINO_LIMIT", "50"))
LOG_TYPES = ["syslog", "authlog"]
JST = timezone(timedelta(hours=9), "JST")

app = Flask(__name__)
app.secret_key = os.getenv("FLASK_SECRET_KEY") or os.getenv("SESSION_SECRET", "dev-secret-key")


class TrinoClient:
    def __init__(self, base_url):
        self.statement_url = urljoin(base_url.rstrip("/") + "/", "v1/statement")

    def ping(self):
        try:
            self.execute("SELECT 1", timeout=5)
            return True
        except requests.RequestException:
            return False
        except RuntimeError:
            return False

    def execute(self, sql, timeout=15):
        response = requests.post(
            self.statement_url,
            data=sql.encode("utf-8"),
            headers=trino_headers(),
            auth=trino_auth(),
            timeout=timeout,
        )
        response.raise_for_status()
        return collect_pages(response.json(), timeout=timeout)


def get_client():
    return TrinoClient(TRINO_URL)


def trino_headers():
    headers = {
        "X-Trino-User": TRINO_USER,
        "X-Trino-Source": "flask-trino-log-search",
        "Content-Type": "text/plain; charset=utf-8",
    }
    if TRINO_CATALOG:
        headers["X-Trino-Catalog"] = TRINO_CATALOG
    if TRINO_SCHEMA:
        headers["X-Trino-Schema"] = TRINO_SCHEMA
    return headers


def trino_auth():
    if TRINO_PASSWORD:
        return (TRINO_USER, TRINO_PASSWORD)
    return None


def collect_pages(body, timeout=15):
    rows = []
    columns = []

    while True:
        if "error" in body:
            message = body["error"].get("message", body["error"])
            raise RuntimeError(f"Trino query failed: {message}")

        rows.extend(body.get("data", []))
        if body.get("columns") and not columns:
            columns = [column["name"] for column in body["columns"]]

        next_uri = body.get("nextUri")
        if not next_uri:
            return rows, columns

        response = requests.get(next_uri, headers=trino_headers(), auth=trino_auth(), timeout=timeout)
        response.raise_for_status()
        body = response.json()


def wait_for_trino(client, retries=30, delay=2):
    for attempt in range(1, retries + 1):
        if client.ping():
            return

        if attempt == retries:
            raise RuntimeError("Trino is not available")
        time.sleep(delay)


def format_timestamp(value):
    if value is None:
        return ""

    if isinstance(value, (int, float)):
        return datetime.fromtimestamp(value / 1000, tz=timezone.utc).astimezone(JST).strftime("%Y/%m/%d %H:%M:%S JST")

    if isinstance(value, datetime):
        if value.tzinfo is None:
            return value.strftime("%Y/%m/%d %H:%M:%S JST")
        return value.astimezone(JST).strftime("%Y/%m/%d %H:%M:%S JST")

    if isinstance(value, str):
        normalized = value.strip().replace(" UTC", "Z").replace(" ", "T").replace("Z", "+00:00")
        try:
            parsed = datetime.fromisoformat(normalized)
            if parsed.tzinfo is None:
                return parsed.strftime("%Y/%m/%d %H:%M:%S JST")
            return parsed.astimezone(JST).strftime("%Y/%m/%d %H:%M:%S JST")
        except ValueError:
            return value

    return str(value)


def normalize_filters(args):
    return {
        "time_from": args.get("time_from", "").strip(),
        "time_to": args.get("time_to", "").strip(),
        "log_type": args.get("log_type", "").strip(),
        "host": args.get("host", "").strip(),
        "program": args.get("program", "").strip(),
        "message": args.get("message", "").strip(),
    }


def filters_from_request():
    if request.is_json:
        return normalize_filters(request.get_json(silent=True) or {})
    if request.method == "POST":
        return normalize_filters(request.form)
    return normalize_filters(request.args)


def today_jst():
    return datetime.now(JST).date()


def time_bound(value, direction, target_date=None):
    target_date = target_date or today_jst()
    if not value:
        boundary = datetime_time(0, 0, 0) if direction == "from" else datetime_time(23, 59, 59)
        return f"{target_date.isoformat()} {boundary.strftime('%H:%M:%S')}"

    normalized = value.strip()
    if "T" in normalized:
        try:
            parsed = datetime.fromisoformat(normalized)
            if parsed.tzinfo is not None:
                parsed = parsed.astimezone(JST).replace(tzinfo=None)
            return parsed.strftime("%Y-%m-%d %H:%M:%S")
        except ValueError:
            pass

    try:
        parsed_time = datetime_time.fromisoformat(add_seconds(normalized))
        return f"{target_date.isoformat()} {parsed_time.strftime('%H:%M:%S')}"
    except ValueError:
        boundary = datetime_time(0, 0, 0) if direction == "from" else datetime_time(23, 59, 59)
        return f"{target_date.isoformat()} {boundary.strftime('%H:%M:%S')}"


def add_seconds(value):
    if len(value.split(":")) == 2:
        return f"{value}:00"
    return value


def build_query(filters):
    selects = [select_for_log_type(log_type, filters) for log_type in target_log_types(filters)]
    union_sql = "\nUNION ALL\n".join(selects)
    return f"SELECT * FROM (\n{union_sql}\n) logs\nORDER BY event_time DESC\nLIMIT {DEFAULT_LIMIT}"


def select_for_log_type(log_type, filters):
    timestamp_sql = timestamp_expression_sql()
    conditions = [
        f"{timestamp_sql} >= TIMESTAMP {sql_string(time_bound(filters['time_from'], 'from'))}",
        f"{timestamp_sql} <= TIMESTAMP {sql_string(time_bound(filters['time_to'], 'to'))}",
    ]

    if filters["host"]:
        conditions.append(equals_condition("host", filters["host"]))
    if filters["program"]:
        conditions.append(equals_condition("program", filters["program"]))
    if filters["message"]:
        conditions.append(like_condition("message", filters["message"]))

    return f"""SELECT
  {timestamp_sql} AS event_time,
  CAST({quoted_identifier("host")} AS varchar) AS host,
  CAST({quoted_identifier("program")} AS varchar) AS program,
  CAST({quoted_identifier("message")} AS varchar) AS msg,
  {sql_string(log_type)} AS log_type
FROM {table_for_log_type(log_type)}
WHERE {" AND ".join(conditions)}"""


def equals_condition(field, value):
    return f"lower(CAST({quoted_identifier(field)} AS varchar)) = lower({sql_string(value)})"


def like_condition(field, value):
    return f"lower(CAST({quoted_identifier(field)} AS varchar)) LIKE lower({sql_string('%' + escape_like(value) + '%')}) ESCAPE '!'"


def target_log_types(filters):
    log_type = filters.get("log_type")
    if log_type in LOG_TYPES:
        return [log_type]
    return LOG_TYPES


def table_for_log_type(log_type):
    if log_type == "syslog":
        return table_expr(TRINO_SYSLOG_TABLE)
    return table_expr(TRINO_AUTHLOG_TABLE)


def table_expr(name):
    parts = [part for part in str(name).split(".") if part]
    if len(parts) == 1:
        parts = [TRINO_CATALOG, TRINO_SCHEMA, name]
    return ".".join(quoted_identifier(part) for part in parts if part)


def timestamp_expression_sql():
    if TRINO_TIMESTAMP_EXPRESSION:
        return TRINO_TIMESTAMP_EXPRESSION
    return quoted_identifier(TRINO_TIMESTAMP_COLUMN)


def quoted_identifier(value):
    return f'"{str(value).replace(chr(34), chr(34) + chr(34))}"'


def sql_string(value):
    return f"'{str(value).replace(chr(39), chr(39) + chr(39))}'"


def escape_like(value):
    return str(value).replace("!", "!!").replace("%", "!%").replace("_", "!_")


def get_filter_options():
    return {
        "log_types": LOG_TYPES,
    }


def search_logs(client, filters):
    query = build_query(filters)
    rows, columns = client.execute(query)

    logs = []
    for row_number, row in enumerate(rows):
        source = dict(zip(columns, row))
        event_time = source.get("event_time")
        log = {
            **source,
            "id": row_number,
            "index": f"{TRINO_CATALOG}.{TRINO_SCHEMA}",
            "display_time": format_timestamp(event_time),
        }
        logs.append(log)
    return logs


@app.route("/", methods=["GET", "POST"])
def index():
    if request.method == "POST":
        session["filters"] = filters_from_request()
        session["searched"] = True
        return redirect(url_for("index"))

    if request.args:
        filters = filters_from_request()
        searched = True
    else:
        searched = session.pop("searched", False)
        filters = normalize_filters(session.pop("filters", {})) if searched else normalize_filters({})

    logs = search_logs(get_client(), filters) if searched else []
    options = get_filter_options()
    return render_template(
        "index.html",
        filters=filters,
        logs=logs,
        options=options,
        searched=searched,
    )


@app.get("/clear")
def clear_filters():
    session.pop("filters", None)
    session.pop("searched", None)
    return redirect(url_for("index"))


@app.get("/health")
def health():
    client = get_client()
    return jsonify(
        {
            "ok": client.ping(),
            "trino_url": TRINO_URL,
            "catalog": TRINO_CATALOG,
            "schema": TRINO_SCHEMA,
        }
    )


@app.route("/api/logs", methods=["GET", "POST"])
def api_search_logs():
    filters = filters_from_request()
    logs = search_logs(get_client(), filters)
    return jsonify({"filters": filters, "count": len(logs), "logs": logs})


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000)
