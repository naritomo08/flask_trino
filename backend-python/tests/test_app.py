from datetime import date

import pytest

import app as flask_app
import trino_backend as log_backend


class FakeTrino:
    def __init__(self):
        self.queries = []

    def ping(self):
        return True

    def execute(self, sql):
        self.queries.append(sql)
        return (
            [
                [
                    "2026-06-02 20:11:55.000",
                    "flink1",
                    "systemd",
                    "Reached target sshd-keygen.target.",
                    "syslog",
                ]
            ],
            ["event_time", "host", "program", "msg", "log_type"],
        )


@pytest.fixture()
def fake_client(monkeypatch):
    client = FakeTrino()
    backend = log_backend.TrinoLogBackend(client_factory=lambda: client)
    monkeypatch.setattr(flask_app, "get_backend", lambda: backend)
    monkeypatch.setattr(log_backend, "today_jst", lambda: date(2026, 6, 2))
    return client


@pytest.fixture()
def flask_client(fake_client):
    flask_app.app.config.update(TESTING=True)
    return flask_app.app.test_client()


def test_format_timestamp_converts_epoch_millis_to_jst():
    assert log_backend.format_timestamp(1780398715000) == "2026/06/02 20:11:55 JST"


def test_format_timestamp_keeps_naive_trino_timestamp_as_jst():
    assert log_backend.format_timestamp("2026-06-02 20:11:55.000") == "2026/06/02 20:11:55 JST"


def test_time_bound_uses_today_jst_for_time_only_input(fake_client):
    assert log_backend.time_bound("20:11", "from") == "2026-06-02 20:11:00"
    assert log_backend.time_bound("", "to") == "2026-06-02 23:59:59"


def test_escape_helpers_quote_sql_safely():
    assert log_backend.sql_string("can't") == "'can''t'"
    assert log_backend.quoted_identifier('bad"name') == '"bad""name"'
    assert log_backend.escape_like("100%!_") == "100!%!!!_"


def test_build_query_with_message_program_host_and_time_range(fake_client):
    filters = {
        "time_from": "20:00",
        "time_to": "21:00",
        "log_type": "syslog",
        "host": "flink1",
        "program": "systemd",
        "message": "sshd",
    }

    query = log_backend.build_query(filters)

    assert 'FROM "iceberg"."logs"."syslog_events"' in query
    assert 'FROM "iceberg"."logs"."authlog_events"' not in query
    assert '"ts" >= TIMESTAMP \'2026-06-02 20:00:00\'' in query
    assert '"ts" <= TIMESTAMP \'2026-06-02 21:00:00\'' in query
    assert 'lower(CAST("host" AS varchar)) = lower(\'flink1\')' in query
    assert 'lower(CAST("program" AS varchar)) = lower(\'systemd\')' in query
    assert 'lower(CAST("message" AS varchar)) LIKE lower(\'%sshd%\') ESCAPE \'!\'' in query
    assert "ORDER BY event_time DESC" in query
    assert "LIMIT 50" in query


def test_build_query_searches_both_log_tables_by_default(fake_client):
    filters = flask_app.normalize_filters({"message": "authlog forward test from"})

    query = log_backend.build_query(filters)

    assert 'FROM "iceberg"."logs"."syslog_events"' in query
    assert 'FROM "iceberg"."logs"."authlog_events"' in query
    assert "UNION ALL" in query
    assert "authlog forward test from" in query


def test_search_logs_executes_sql_and_formats_result(fake_client):
    filters = {
        "time_from": "",
        "time_to": "",
        "log_type": "syslog",
        "host": "",
        "program": "systemd",
        "message": "sshd",
    }

    logs = log_backend.search_logs(fake_client, filters)

    query = fake_client.queries[0]
    assert 'FROM "iceberg"."logs"."syslog_events"' in query
    assert 'FROM "iceberg"."logs"."authlog_events"' not in query
    assert logs[0]["display_time"] == "2026/06/02 20:11:55 JST"
    assert logs[0]["log_type"] == "syslog"
    assert logs[0]["msg"] == "Reached target sshd-keygen.target."


def test_post_index_search_keeps_filters_in_body(flask_client):
    response = flask_client.post("/", data={"program": "systemd", "message": "sshd"})

    assert response.status_code == 302
    assert response.headers["Location"] == "/"

    response = flask_client.get("/")
    assert response.status_code == 200
    html = response.get_data(as_text=True)
    assert 'method="post"' in html
    assert 'id="search-form"' in html
    assert 'id="results-summary"' in html
    assert 'id="results-body"' in html
    assert 'src="/static/search.js"' in html
    assert 'type="time"' in html
    assert 'value="systemd"' in html
    assert 'value="sshd"' in html
    assert "2026/06/02 20:11:55 JST" in html

    response = flask_client.get("/")
    html = response.get_data(as_text=True)
    assert 'value="systemd"' not in html
    assert 'value="sshd"' not in html
    assert "2026/06/02 20:11:55 JST" not in html
    assert "検索を実施してください" in html


def test_clear_filters_removes_session_filters(flask_client):
    flask_client.post("/", data={"program": "systemd", "message": "sshd"})

    response = flask_client.get("/clear")
    assert response.status_code == 302
    assert response.headers["Location"] == "/"

    response = flask_client.get("/")
    html = response.get_data(as_text=True)
    assert 'value="systemd"' not in html
    assert 'value="sshd"' not in html
    assert "2026/06/02 20:11:55 JST" not in html
    assert "検索を実施してください" in html


def test_empty_post_search_runs_all_logs_once_then_reload_resets(flask_client, fake_client):
    response = flask_client.post("/", data={})

    assert response.status_code == 302

    response = flask_client.get("/")
    html = response.get_data(as_text=True)
    assert "1 件" in html
    assert "2026/06/02 20:11:55 JST" in html
    assert 'FROM "iceberg"."logs"."syslog_events"' in fake_client.queries[-1]
    assert 'FROM "iceberg"."logs"."authlog_events"' in fake_client.queries[-1]

    response = flask_client.get("/")
    html = response.get_data(as_text=True)
    assert "検索を実施してください" in html
    assert "2026/06/02 20:11:55 JST" not in html


def test_post_api_logs_accepts_json(flask_client):
    response = flask_client.post("/api/logs", json={"program": "systemd", "message": "sshd"})

    assert response.status_code == 200
    payload = response.get_json()
    assert payload["count"] == 1
    assert payload["filters"]["program"] == "systemd"
    assert payload["logs"][0]["display_time"] == "2026/06/02 20:11:55 JST"


def test_post_api_logs_with_empty_filters_searches_all_logs(flask_client, fake_client):
    response = flask_client.post("/api/logs", json={})

    assert response.status_code == 200
    payload = response.get_json()
    assert payload["count"] == 1
    assert payload["logs"][0]["display_time"] == "2026/06/02 20:11:55 JST"
    assert 'FROM "iceberg"."logs"."syslog_events"' in fake_client.queries[-1]
    assert 'FROM "iceberg"."logs"."authlog_events"' in fake_client.queries[-1]
