import os

from flask import Flask, jsonify, redirect, render_template, request, session, url_for

from backend_factory import create_backend


app = Flask(__name__)
app.secret_key = os.getenv("FLASK_SECRET_KEY") or os.getenv("SESSION_SECRET", "dev-secret-key")
backend = create_backend()


def get_backend():
    return backend


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

    current_backend = get_backend()
    logs = current_backend.search_logs(filters) if searched else []
    options = current_backend.get_filter_options()
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
    current_backend = get_backend()
    return jsonify(
        {
            "ok": current_backend.ping(),
            **current_backend.health_info(),
        }
    )


@app.route("/api/logs", methods=["GET", "POST"])
def api_search_logs():
    filters = filters_from_request()
    logs = get_backend().search_logs(filters)
    return jsonify({"filters": filters, "count": len(logs), "logs": logs})


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000)
