require "json"
require "sinatra/base"

require_relative "log_search"
require_relative "trino_client"

class LogSearchApp < Sinatra::Base
  include LogSearch

  TRINO_URL = ENV.fetch("TRINO_URL", "http://trino1:8080")
  TRINO_USER = ENV.fetch("TRINO_USER", "log_search")
  TRINO_PASSWORD = ENV.fetch("TRINO_PASSWORD", "")
  TRINO_CATALOG = ENV.fetch("TRINO_CATALOG", "iceberg")
  TRINO_SCHEMA = ENV.fetch("TRINO_SCHEMA", "logs")
  TRINO_SYSLOG_TABLE = ENV.fetch("TRINO_SYSLOG_TABLE", "syslog_events")
  TRINO_AUTHLOG_TABLE = ENV.fetch("TRINO_AUTHLOG_TABLE", "authlog_events")
  TRINO_SYSLOG_HOST_1M_TABLE = ENV.fetch("TRINO_SYSLOG_HOST_1M_TABLE", "syslog_host_1m")
  TRINO_AUTHLOG_HOST_1M_TABLE = ENV.fetch("TRINO_AUTHLOG_HOST_1M_TABLE", "authlog_host_1m")
  TRINO_TIMESTAMP_COLUMN = ENV.fetch("TRINO_TIMESTAMP_COLUMN", "ts")
  TRINO_TIMESTAMP_EXPRESSION = ENV.fetch("TRINO_TIMESTAMP_EXPRESSION", "")
  LOG_TYPES = %w[syslog authlog].freeze
  TRINO_UNAVAILABLE = {
    error: "Trinoに接続できませんでした。稼働状況を確認して、もう一度お試しください。",
    code: "trino_unavailable"
  }.freeze

  get "/" do
    json_response(
      service: "ruby-trino-backend",
      endpoints: ["/health", "/api/options", "/api/logs", "/api/summary"]
    )
  end

  get "/health" do
    json_response(
      ok: client.ping,
      trino_url: TRINO_URL,
      catalog: TRINO_CATALOG,
      schema: TRINO_SCHEMA
    )
  end

  get "/api/options" do
    json_response(log_types: LOG_TYPES)
  end

  get "/api/summary" do
    json_response(get_log_total(client, params.fetch("date", "")))
  rescue StandardError => e
    warn "Trino log summary failed: #{e.full_message}"
    status 502
    json_response(TRINO_UNAVAILABLE)
  end

  get "/api/logs" do
    api_search_logs(filters_from_hash(params))
  end

  post "/api/logs" do
    filters =
      if request.media_type == "application/json"
        body = request.body.read
        filters_from_hash(body.empty? ? {} : JSON.parse(body))
      else
        filters_from_hash(params)
      end

    api_search_logs(filters)
  end

  def api_search_logs(filters)
    result = search_logs_page(client, filters)
    json_response(filters: filters, **result)
  rescue StandardError => e
    warn "Trino log search failed: #{e.full_message}"
    status 502
    json_response(TRINO_UNAVAILABLE)
  end

  def json_response(payload)
    content_type :json
    JSON.generate(payload)
  end

  def client
    return settings.trino_client if settings.respond_to?(:trino_client) && settings.trino_client

    TrinoClient.new(
      TRINO_URL,
      user: TRINO_USER,
      password: TRINO_PASSWORD,
      catalog: TRINO_CATALOG,
      schema: TRINO_SCHEMA
    )
  end

  run! if app_file == $PROGRAM_NAME
end
