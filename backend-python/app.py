from flask import Flask, jsonify, request

from backend_factory import create_backend


app = Flask(__name__)
backend = create_backend()


def get_backend():
    return backend


def normalize_filters(args):
    return {
        "date": args.get("date", "").strip(),
        "time_from": args.get("time_from", "").strip(),
        "time_to": args.get("time_to", "").strip(),
        "log_type": args.get("log_type", "").strip(),
        "host": args.get("host", "").strip(),
        "program": args.get("program", "").strip(),
        "message": args.get("message", "").strip(),
        "page": positive_int(args.get("page"), 1),
        "size": min(positive_int(args.get("size"), 25), 100),
    }


def positive_int(value, fallback):
    try:
        parsed = int(value)
        return parsed if parsed > 0 else fallback
    except (TypeError, ValueError):
        return fallback


def filters_from_request():
    if request.is_json:
        return normalize_filters(request.get_json(silent=True) or {})
    if request.method == "POST":
        return normalize_filters(request.form)
    return normalize_filters(request.args)


@app.get("/")
def index():
    return jsonify(
        {
            "service": "flask-trino-backend",
            "endpoints": ["/health", "/api/options", "/api/logs"],
        }
    )


@app.get("/health")
def health():
    current_backend = get_backend()
    return jsonify(
        {
            "ok": current_backend.ping(),
            **current_backend.health_info(),
        }
    )


@app.get("/api/options")
def api_options():
    return jsonify(get_backend().get_filter_options())


@app.route("/api/logs", methods=["GET", "POST"])
def api_search_logs():
    filters = filters_from_request()
    try:
        result = get_backend().search_logs_page(filters)
    except Exception as error:
        return jsonify({"error": str(error)}), 502
    return jsonify({"filters": filters, **result})


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000)
