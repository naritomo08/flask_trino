import os
import time
from datetime import datetime, timedelta, timezone

from elasticsearch import Elasticsearch
from elasticsearch.exceptions import ConnectionError as ElasticsearchConnectionError
from flask import Flask, jsonify, redirect, render_template, request, session, url_for


INDEX_PATTERN = os.getenv("ELASTICSEARCH_INDEX", "logs-*")
ELASTICSEARCH_URL = os.getenv("ELASTICSEARCH_URL", "http://elastic1:9200")
LOG_TYPES = ["syslog", "authlog"]
JST = timezone(timedelta(hours=9), "JST")

app = Flask(__name__)
app.secret_key = os.getenv("FLASK_SECRET_KEY", "dev-secret-key")


def get_client():
    return Elasticsearch(ELASTICSEARCH_URL, request_timeout=10)


def wait_for_elasticsearch(client, retries=30, delay=2):
    for attempt in range(1, retries + 1):
        try:
            if client.ping():
                return
        except ElasticsearchConnectionError:
            pass

        if attempt == retries:
            raise RuntimeError("Elasticsearch is not available")
        time.sleep(delay)


def format_timestamp(value):
    if value is None:
        return ""

    if isinstance(value, (int, float)):
        return datetime.fromtimestamp(value / 1000, tz=timezone.utc).astimezone(JST).strftime("%Y/%m/%d %H:%M:%S JST")

    if isinstance(value, str):
        normalized = value.replace("Z", "+00:00")
        try:
            parsed = datetime.fromisoformat(normalized)
            if parsed.tzinfo is None:
                parsed = parsed.replace(tzinfo=timezone.utc)
            return parsed.astimezone(JST).strftime("%Y/%m/%d %H:%M:%S JST")
        except ValueError:
            return value

    return str(value)


def detect_log_type(index_name):
    if "authlog" in index_name:
        return "authlog"
    if "syslog" in index_name:
        return "syslog"
    return "unknown"


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


def datetime_local_to_iso(value):
    if not value:
        return ""
    try:
        return datetime.fromisoformat(value).replace(tzinfo=JST).astimezone(timezone.utc).isoformat()
    except ValueError:
        return value


def wildcard_value(value):
    escaped = value.replace("\\", "\\\\").replace("*", "\\*").replace("?", "\\?")
    return f"*{escaped}*"


def text_search_clause(field, value):
    return {
        "bool": {
            "should": [
                {"match_phrase": {field: {"query": value}}},
                {"match": {field: {"query": value, "operator": "and"}}},
                {"wildcard": {field: {"value": wildcard_value(value), "case_insensitive": True}}},
                {"wildcard": {f"{field}.keyword": {"value": wildcard_value(value), "case_insensitive": True}}},
            ],
            "minimum_should_match": 1,
        }
    }


def exact_match_clause(field, value):
    return {
        "bool": {
            "should": [
                {"term": {f"{field}.keyword": {"value": value}}},
                {"term": {field: {"value": value}}},
            ],
            "minimum_should_match": 1,
        }
    }


def log_matches_exact_filters(log, filters):
    for field in ("host", "program"):
        expected = filters[field]
        if expected and log.get(field) != expected:
            return False
    return True


def build_query(filters):
    must = []
    filter_clauses = []

    if filters["message"]:
        must.append(text_search_clause("msg", filters["message"]))
    if filters["program"]:
        filter_clauses.append(exact_match_clause("program", filters["program"]))

    if filters["host"]:
        filter_clauses.append(exact_match_clause("host", filters["host"]))

    time_range = {}
    if filters["time_from"]:
        time_range["gte"] = datetime_local_to_iso(filters["time_from"])
    if filters["time_to"]:
        time_range["lte"] = datetime_local_to_iso(filters["time_to"])
    if time_range:
        filter_clauses.append({"range": {"@timestamp": time_range}})

    if not must and not filter_clauses:
        query = {"match_all": {}}
    else:
        query = {"bool": {}}
        if must:
            query["bool"]["must"] = must
        if filter_clauses:
            query["bool"]["filter"] = filter_clauses

    return query


def index_pattern_for_log_type(log_type):
    if log_type in LOG_TYPES:
        return f"logs-{log_type}-*"
    return INDEX_PATTERN


def get_filter_options():
    return {
        "log_types": LOG_TYPES,
    }


def search_logs(client, filters):
    query = build_query(filters)

    response = client.search(
        index=index_pattern_for_log_type(filters["log_type"]),
        query=query,
        sort=[{"@timestamp": {"order": "desc", "unmapped_type": "date"}}],
        size=50,
        ignore_unavailable=True,
        track_total_hits=False,
        request_timeout=10,
        timeout="5s",
        source=["@timestamp", "host", "program", "msg", "severity", "dt", "hr"],
    )

    logs = []
    for hit in response["hits"]["hits"]:
        source = hit["_source"]
        log = {
            **source,
            "id": hit["_id"],
            "index": hit["_index"],
            "log_type": detect_log_type(hit["_index"]),
            "display_time": format_timestamp(source.get("@timestamp")),
            "score": hit.get("_score"),
        }
        if log_matches_exact_filters(log, filters):
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
    return jsonify({"ok": client.ping(), "elasticsearch_url": ELASTICSEARCH_URL, "index": INDEX_PATTERN})


@app.route("/api/logs", methods=["GET", "POST"])
def api_search_logs():
    filters = filters_from_request()
    logs = search_logs(get_client(), filters)
    return jsonify({"filters": filters, "count": len(logs), "logs": logs})


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000)
