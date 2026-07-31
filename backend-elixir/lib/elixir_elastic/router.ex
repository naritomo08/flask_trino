defmodule ElixirElastic.Router do
  @moduledoc false

  use Plug.Router
  require Logger

  alias ElixirElastic.TrinoSearch

  plug(Plug.Parsers,
    parsers: [:urlencoded, :json],
    pass: ["application/json"],
    json_decoder: Jason
  )

  plug(:match)
  plug(:dispatch)

  get "/" do
    json(conn, %{
      service: "elixir-trino-backend",
      endpoints: ["/health", "/api/options", "/api/logs", "/api/summary"]
    })
  end

  get "/health" do
    json(conn, %{
      ok: TrinoSearch.ping(),
      trino_url: Application.fetch_env!(:elixir_elastic, :trino_url),
      catalog: Application.fetch_env!(:elixir_elastic, :trino_catalog),
      schema: Application.fetch_env!(:elixir_elastic, :trino_schema),
      syslog_table: Application.fetch_env!(:elixir_elastic, :syslog_table),
      authlog_table: Application.fetch_env!(:elixir_elastic, :authlog_table),
      timestamp_column: Application.fetch_env!(:elixir_elastic, :timestamp_column),
      timestamp_expression: Application.fetch_env!(:elixir_elastic, :timestamp_expression)
    })
  end

  get "/api/options" do
    json(conn, %{log_types: TrinoSearch.log_types()})
  end

  get "/api/logs" do
    conn = fetch_query_params(conn)
    filters = normalize_filters(conn.query_params)
    api_search_logs(conn, filters)
  end

  get "/api/summary" do
    conn = fetch_query_params(conn)

    try do
      json(conn, TrinoSearch.get_log_total(conn.query_params["date"]))
    rescue
      exception ->
        Logger.warning("Trino log summary failed: #{Exception.message(exception)}")
        json(conn, 502, %{error: "Trinoに接続できませんでした。稼働状況を確認して、もう一度お試しください。", code: "trino_unavailable"})
    end
  end

  post "/api/logs" do
    filters = normalize_filters(conn.body_params)
    api_search_logs(conn, filters)
  end

  match _ do
    send_resp(conn, 404, "Not found")
  end

  def normalize_filters(params) do
    %{
      "date" => clean(params["date"]),
      "time_from" => clean(params["time_from"]),
      "time_to" => clean(params["time_to"]),
      "log_type" => clean(params["log_type"]),
      "host" => clean(params["host"]),
      "program" => clean(params["program"]),
      "message" => clean(params["message"]),
      "page" => positive_int(params["page"], 1),
      "size" => min(positive_int(params["size"], 25), 100),
      "skip_total" => String.downcase(to_string(params["skip_total"] || "")) in ["1", "true"]
    }
  end

  defp clean(nil), do: ""
  defp clean(value), do: String.trim(to_string(value))

  defp positive_int(value, fallback) do
    case Integer.parse(to_string(value || "")) do
      {number, ""} when number > 0 -> number
      _ -> fallback
    end
  end

  defp json(conn, payload) do
    conn
    |> put_resp_content_type("application/json")
    |> send_resp(200, Jason.encode!(payload))
  end

  defp json(conn, status, payload) do
    conn
    |> put_resp_content_type("application/json")
    |> send_resp(status, Jason.encode!(payload))
  end

  defp api_search_logs(conn, filters) do
    try do
      result = TrinoSearch.search_logs_page(filters)
      json(conn, Map.put(result, :filters, filters))
    rescue
      exception ->
        Logger.warning("Trino log search failed: #{Exception.message(exception)}")

        json(conn, 502, %{
          error: "Trinoに接続できませんでした。稼働状況を確認して、もう一度お試しください。",
          code: "trino_unavailable"
        })
    end
  end

end
