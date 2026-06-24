import json
import os
import time
import urllib.error
import urllib.request

import pytest


DEFAULT_TARGETS = {
    "python": "http://backend-python:5000",
    "go": "http://backend-go:5000",
    "java": "http://backend-java:5000",
    "php": "http://backend-php:5000",
    "ruby": "http://backend-ruby:5000",
    "elixir": "http://backend-elixir:5000",
}


def configured_targets():
    raw = os.getenv("BACKEND_TARGETS", "")
    if not raw:
        return DEFAULT_TARGETS

    targets = {}
    for item in raw.split(","):
        if not item.strip():
            continue
        name, url = item.split("=", 1)
        targets[name.strip()] = url.strip().rstrip("/")
    return targets


BACKENDS = configured_targets()


@pytest.fixture(params=BACKENDS.items(), ids=lambda item: item[0])
def backend(request):
    name, base_url = request.param
    wait_for_backend(base_url)
    return name, base_url


def wait_for_backend(base_url):
    last_error = None
    for _ in range(30):
        try:
            request_json(base_url + "/")
            return
        except Exception as error:
            last_error = error
            time.sleep(1)
    raise AssertionError(f"{base_url} did not become ready: {last_error}")


def request_json(url, method="GET", payload=None, timeout=10):
    data = None
    headers = {"Accept": "application/json"}
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"

    request = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            body = response.read().decode("utf-8")
            content_type = response.headers.get("Content-Type", "")
            assert "application/json" in content_type
            return response.status, json.loads(body)
    except urllib.error.HTTPError as error:
        body = error.read().decode("utf-8")
        raise AssertionError(f"{url} returned HTTP {error.code}: {body}") from error


def test_backend_root_describes_common_endpoints(backend):
    name, base_url = backend

    status, payload = request_json(base_url + "/")

    assert status == 200
    assert payload["service"].endswith("-trino-backend")
    assert "/health" in payload["endpoints"]
    assert "/api/options" in payload["endpoints"]
    assert "/api/logs" in payload["endpoints"]


def test_backend_options_contract_is_common(backend):
    name, base_url = backend

    status, payload = request_json(base_url + "/api/options")

    assert status == 200
    assert payload == {"log_types": ["syslog", "authlog"]}


def test_backend_health_contract_is_common(backend):
    name, base_url = backend

    status, payload = request_json(base_url + "/health")

    assert status == 200
    assert isinstance(payload["ok"], bool)
    assert payload["trino_url"]
    assert payload["catalog"]
    assert payload["schema"]


@pytest.mark.skipif(
    os.getenv("RUN_SEARCH_CONTRACT_TESTS") != "1",
    reason="set RUN_SEARCH_CONTRACT_TESTS=1 when Trino is reachable",
)
def test_backend_logs_contract_is_common_when_trino_is_available(backend):
    name, base_url = backend

    status, payload = request_json(
        base_url + "/api/logs",
        method="POST",
        payload={
            "date": "2026-06-19",
            "message": "",
            "log_type": "",
            "host": "",
            "program": "",
            "page": 2,
            "size": 10,
        },
        timeout=60,
    )

    assert status == 200
    assert set(payload) == {"filters", "count", "total", "page", "size", "total_pages", "logs"}
    assert isinstance(payload["count"], int)
    assert isinstance(payload["total"], int)
    assert payload["page"] == 2
    assert payload["size"] == 10
    assert payload["total_pages"] >= 1
    assert isinstance(payload["logs"], list)
    if payload["logs"]:
        first = payload["logs"][0]
        assert first["id"] >= 10
        assert "display_time" in first
        assert "log_type" in first
        assert "host" in first
        assert "program" in first
        assert "msg" in first
