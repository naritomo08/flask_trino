import logging

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse

from backend_factory import create_backend
from trino_backend import get_log_total


app = FastAPI()
backend = create_backend()
logger = logging.getLogger(__name__)

TRINO_UNAVAILABLE = {
    "error": "Trinoに接続できませんでした。稼働状況を確認して、もう一度お試しください。",
    "code": "trino_unavailable",
}


def get_backend():
    return backend


def normalize_filters(args):
    return {
        "date": str(args.get("date", "")).strip(),
        "time_from": str(args.get("time_from", "")).strip(),
        "time_to": str(args.get("time_to", "")).strip(),
        "log_type": str(args.get("log_type", "")).strip(),
        "host": str(args.get("host", "")).strip(),
        "program": str(args.get("program", "")).strip(),
        "message": str(args.get("message", "")).strip(),
        "page": positive_int(args.get("page"), 1),
        "size": min(positive_int(args.get("size"), 25), 100),
        "skip_total": str(args.get("skip_total", "")).lower() in ("1", "true"),
    }


def positive_int(value, fallback):
    try:
        parsed = int(value)
        return parsed if parsed > 0 else fallback
    except (TypeError, ValueError):
        return fallback


async def filters_from_request(request: Request):
    if request.method == "POST":
        content_type = request.headers.get("content-type", "")
        if "application/json" in content_type:
            body = await request.json()
            return normalize_filters(body or {})
        if "application/x-www-form-urlencoded" in content_type or "multipart/form-data" in content_type:
            form = await request.form()
            return normalize_filters(form)
        return normalize_filters({})
    return normalize_filters(request.query_params)


@app.get("/")
def index():
    return {
        "service": "python-trino-backend",
        "endpoints": ["/health", "/api/options", "/api/logs", "/api/summary"],
    }


@app.get("/health")
def health():
    current_backend = get_backend()
    return {
        "ok": current_backend.ping(),
        **current_backend.health_info(),
    }


@app.get("/api/options")
def api_options():
    return get_backend().get_filter_options()


@app.api_route("/api/logs", methods=["GET", "POST"])
async def api_search_logs(request: Request):
    filters = await filters_from_request(request)
    try:
        result = get_backend().search_logs_page(filters)
    except Exception:
        logger.exception("Trino log search failed")
        return JSONResponse(TRINO_UNAVAILABLE, status_code=502)
    return {"filters": filters, **result}


@app.get("/api/summary")
def api_summary(date: str = ""):
    try:
        return get_log_total(get_backend().client_factory(), date.strip())
    except Exception:
        logger.exception("Trino log summary failed")
        return JSONResponse(TRINO_UNAVAILABLE, status_code=502)
