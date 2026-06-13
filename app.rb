require "base64"
require "date"
require "json"
require "net/http"
require "sinatra/base"
require "time"
require "uri"

class TrinoClient
  def initialize(base_url)
    base = base_url.end_with?("/") ? base_url : "#{base_url}/"
    @statement_uri = URI.join(base, "v1/statement")
  end

  def ping
    execute("SELECT 1", timeout: 5)
    true
  rescue StandardError
    false
  end

  def execute(sql, timeout: 15)
    response = request(:post, @statement_uri, timeout: timeout, body: sql)
    collect_pages(JSON.parse(response.body), timeout: timeout)
  end

  private

  def collect_pages(body, timeout:)
    rows = []
    columns = []

    loop do
      if body["error"]
        message = body["error"]["message"] || body["error"].to_s
        raise "Trino query failed: #{message}"
      end

      rows.concat(body.fetch("data", []))
      columns = body["columns"].map { |column| column["name"] } if body["columns"] && columns.empty?

      next_uri = body["nextUri"]
      return [rows, columns] unless next_uri

      response = request(:get, URI(next_uri), timeout: timeout)
      body = JSON.parse(response.body)
    end
  end

  def request(method, uri, timeout:, body: nil)
    http = Net::HTTP.new(uri.host, uri.port)
    http.use_ssl = uri.scheme == "https"
    http.open_timeout = timeout
    http.read_timeout = timeout

    request = method == :post ? Net::HTTP::Post.new(uri) : Net::HTTP::Get.new(uri)
    trino_headers.each { |key, value| request[key] = value }
    request.body = body if body

    response = http.request(request)
    raise "Trino HTTP #{response.code}: #{response.body}" unless response.is_a?(Net::HTTPSuccess)

    response
  end

  def trino_headers
    headers = {
      "X-Trino-User" => LogSearchApp::TRINO_USER,
      "X-Trino-Source" => "ruby-sinatra-trino-log-search",
      "Content-Type" => "text/plain; charset=utf-8"
    }
    headers["X-Trino-Catalog"] = LogSearchApp::TRINO_CATALOG unless LogSearchApp::TRINO_CATALOG.empty?
    headers["X-Trino-Schema"] = LogSearchApp::TRINO_SCHEMA unless LogSearchApp::TRINO_SCHEMA.empty?

    unless LogSearchApp::TRINO_PASSWORD.empty?
      token = Base64.strict_encode64("#{LogSearchApp::TRINO_USER}:#{LogSearchApp::TRINO_PASSWORD}")
      headers["Authorization"] = "Basic #{token}"
    end

    headers
  end
end

class LogSearchApp < Sinatra::Base
  TRINO_URL = ENV.fetch("TRINO_URL", "http://trino1:8080")
  TRINO_USER = ENV.fetch("TRINO_USER", "log_search")
  TRINO_PASSWORD = ENV.fetch("TRINO_PASSWORD", "")
  TRINO_CATALOG = ENV.fetch("TRINO_CATALOG", "iceberg")
  TRINO_SCHEMA = ENV.fetch("TRINO_SCHEMA", "logs")
  TRINO_SYSLOG_TABLE = ENV.fetch("TRINO_SYSLOG_TABLE", "syslog_events")
  TRINO_AUTHLOG_TABLE = ENV.fetch("TRINO_AUTHLOG_TABLE", "authlog_events")
  TRINO_TIMESTAMP_COLUMN = ENV.fetch("TRINO_TIMESTAMP_COLUMN", "ts")
  TRINO_TIMESTAMP_EXPRESSION = ENV.fetch("TRINO_TIMESTAMP_EXPRESSION", "")
  DEFAULT_LIMIT = Integer(ENV.fetch("TRINO_LIMIT", "50"))
  LOG_TYPES = %w[syslog authlog].freeze
  JST_OFFSET = "+09:00"

  configure do
    set :public_folder, File.expand_path("static", __dir__)
  end

  get "/" do
    send_file File.join(settings.public_folder, "index.html")
  end

  post "/" do
    redirect "/"
  end

  get "/health" do
    json_response(
      ok: client.ping,
      trino_url: TRINO_URL,
      catalog: TRINO_CATALOG,
      schema: TRINO_SCHEMA
    )
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
    logs = search_logs(client, filters)
    json_response(filters: filters, count: logs.length, logs: logs)
  rescue StandardError => e
    status 500
    json_response(error: e.message)
  end

  def json_response(payload)
    content_type :json
    JSON.generate(payload)
  end

  def client
    if settings.respond_to?(:trino_client) && settings.trino_client
      settings.trino_client
    else
      TrinoClient.new(TRINO_URL)
    end
  end

  def normalize_filters(source)
    {
      "time_from" => source.fetch("time_from", "").to_s.strip,
      "time_to" => source.fetch("time_to", "").to_s.strip,
      "log_type" => source.fetch("log_type", "").to_s.strip,
      "host" => source.fetch("host", "").to_s.strip,
      "program" => source.fetch("program", "").to_s.strip,
      "message" => source.fetch("message", "").to_s.strip
    }
  end

  def filters_from_hash(source)
    normalize_filters(source.transform_keys(&:to_s))
  end

  def today_jst
    Time.now.getlocal(JST_OFFSET).to_date
  end

  def time_bound(value, direction, target_date = nil)
    target_date ||= today_jst
    return "#{target_date.iso8601} #{boundary_time(direction)}" if value.to_s.empty?

    normalized = value.to_s.strip
    if normalized.include?("T")
      parsed = parse_time(normalized)
      return parsed.getlocal(JST_OFFSET).strftime("%Y-%m-%d %H:%M:%S") if parsed
    end

    with_seconds = add_seconds(normalized)
    return "#{target_date.iso8601} #{with_seconds}" if with_seconds.match?(/\A\d{2}:\d{2}:\d{2}\z/)

    "#{target_date.iso8601} #{boundary_time(direction)}"
  end

  def boundary_time(direction)
    direction == "from" ? "00:00:00" : "23:59:59"
  end

  def add_seconds(value)
    value.split(":").length == 2 ? "#{value}:00" : value
  end

  def build_query(filters)
    selects = target_log_types(filters).map { |log_type| select_for_log_type(log_type, filters) }
    "SELECT * FROM (\n#{selects.join("\nUNION ALL\n")}\n) logs\nORDER BY event_time DESC\nLIMIT #{DEFAULT_LIMIT}"
  end

  def select_for_log_type(log_type, filters)
    timestamp_sql = timestamp_expression_sql
    conditions = [
      "#{timestamp_sql} >= TIMESTAMP #{sql_string(time_bound(filters["time_from"], "from"))}",
      "#{timestamp_sql} <= TIMESTAMP #{sql_string(time_bound(filters["time_to"], "to"))}"
    ]
    conditions << equals_condition("host", filters["host"]) unless filters["host"].empty?
    conditions << equals_condition("program", filters["program"]) unless filters["program"].empty?
    conditions << like_condition("message", filters["message"]) unless filters["message"].empty?

    <<~SQL.chomp
      SELECT
        #{timestamp_sql} AS event_time,
        CAST(#{quoted_identifier("host")} AS varchar) AS host,
        CAST(#{quoted_identifier("program")} AS varchar) AS program,
        CAST(#{quoted_identifier("message")} AS varchar) AS msg,
        #{sql_string(log_type)} AS log_type
      FROM #{table_for_log_type(log_type)}
      WHERE #{conditions.join(" AND ")}
    SQL
  end

  def equals_condition(field, value)
    "lower(CAST(#{quoted_identifier(field)} AS varchar)) = lower(#{sql_string(value)})"
  end

  def like_condition(field, value)
    "lower(CAST(#{quoted_identifier(field)} AS varchar)) LIKE lower(#{sql_string("%#{escape_like(value)}%")}) ESCAPE '!'"
  end

  def target_log_types(filters)
    LOG_TYPES.include?(filters["log_type"]) ? [filters["log_type"]] : LOG_TYPES
  end

  def table_for_log_type(log_type)
    table_expr(log_type == "syslog" ? TRINO_SYSLOG_TABLE : TRINO_AUTHLOG_TABLE)
  end

  def table_expr(name)
    parts = name.to_s.split(".").reject(&:empty?)
    parts = [TRINO_CATALOG, TRINO_SCHEMA, name] if parts.length == 1
    parts.reject(&:empty?).map { |part| quoted_identifier(part) }.join(".")
  end

  def timestamp_expression_sql
    TRINO_TIMESTAMP_EXPRESSION.empty? ? quoted_identifier(TRINO_TIMESTAMP_COLUMN) : TRINO_TIMESTAMP_EXPRESSION
  end

  def quoted_identifier(value)
    "\"#{value.to_s.gsub("\"", "\"\"")}\""
  end

  def sql_string(value)
    "'#{value.to_s.gsub("'", "''")}'"
  end

  def escape_like(value)
    value.to_s.gsub("!", "!!").gsub("%", "!%").gsub("_", "!_")
  end

  def search_logs(trino_client, filters)
    rows, columns = trino_client.execute(build_query(filters))
    rows.each_with_index.map do |row, index|
      source = columns.zip(row).to_h
      source.merge(
        "id" => index,
        "index" => "#{TRINO_CATALOG}.#{TRINO_SCHEMA}",
        "display_time" => format_timestamp(source["event_time"])
      )
    end
  end

  def format_timestamp(value)
    return "" if value.nil?
    return Time.at(value / 1000.0).getlocal(JST_OFFSET).strftime("%Y/%m/%d %H:%M:%S JST") if value.is_a?(Numeric)

    parsed = parse_time(value.to_s.strip)
    parsed ? parsed.getlocal(JST_OFFSET).strftime("%Y/%m/%d %H:%M:%S JST") : value.to_s
  end

  def parse_time(value)
    normalized = value.sub(/ UTC\z/, "Z").tr(" ", "T")
    with_zone = normalized.sub(/Z\z/, "+00:00")
    return Time.iso8601(with_zone) if with_zone.match?(/(?:[+-]\d{2}:?\d{2})\z/)

    match = normalized.match(/\A(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2})(?:\.\d+)?)?\z/)
    return nil unless match

    year, month, day, hour, minute, second = match.captures
    Time.new(year.to_i, month.to_i, day.to_i, hour.to_i, minute.to_i, second.to_i, JST_OFFSET)
  rescue ArgumentError
    nil
  end

  run! if app_file == $PROGRAM_NAME
end
