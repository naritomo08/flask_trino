require "date"
require "time"

module LogSearch
  JST_OFFSET = "+09:00"

  def normalize_filters(source)
    {
      "date" => source.fetch("date", "").to_s.strip,
      "time_from" => source.fetch("time_from", "").to_s.strip,
      "time_to" => source.fetch("time_to", "").to_s.strip,
      "log_type" => source.fetch("log_type", "").to_s.strip,
      "host" => source.fetch("host", "").to_s.strip,
      "program" => source.fetch("program", "").to_s.strip,
      "message" => source.fetch("message", "").to_s.strip,
      "page" => positive_int(source.fetch("page", 1), 1),
      "size" => [positive_int(source.fetch("size", 25), 25), 100].min
    }
  end

  def filters_from_hash(source)
    normalize_filters(source.transform_keys(&:to_s))
  end

  def today_jst
    Time.now.getlocal(JST_OFFSET).to_date
  end

  def target_date(filters)
    Date.iso8601(filters["date"])
  rescue Date::Error
    today_jst
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
    size = filters["size"]
    offset = (filters["page"] - 1) * size
    "SELECT logs.*, count(*) OVER() AS total_count FROM (\n#{union_query(filters)}\n) logs\nORDER BY event_time DESC\nOFFSET #{offset}\nLIMIT #{size}"
  end

  def build_count_query(filters)
    "SELECT count(*) AS total FROM (\n#{union_query(filters)}\n) logs"
  end

  def union_query(filters)
    target_log_types(filters).map { |log_type| select_for_log_type(log_type, filters) }.join("\nUNION ALL\n")
  end

  def select_for_log_type(log_type, filters)
    timestamp_sql = timestamp_expression_sql
    date = target_date(filters)
    conditions = [
      "#{timestamp_sql} >= TIMESTAMP #{sql_string(time_bound(filters["time_from"], "from", date))}",
      "#{timestamp_sql} <= TIMESTAMP #{sql_string(time_bound(filters["time_to"], "to", date))}"
    ]
    conditions << match_condition("host", filters["host"]) unless filters["host"].empty?
    conditions << match_condition("program", filters["program"]) unless filters["program"].empty?
    conditions << like_condition("msg", filters["message"]) unless filters["message"].empty?

    <<~SQL.chomp
      SELECT
        #{timestamp_sql} AS event_time,
        CAST(#{quoted_identifier("host")} AS varchar) AS host,
        CAST(#{quoted_identifier("program")} AS varchar) AS program,
        CAST(#{quoted_identifier("msg")} AS varchar) AS msg,
        #{sql_string(log_type)} AS log_type
      FROM #{table_for_log_type(log_type)}
      WHERE #{conditions.join(" AND ")}
    SQL
  end

  def equals_condition(field, value)
    "lower(CAST(#{quoted_identifier(field)} AS varchar)) = lower(#{sql_string(value)})"
  end

  def match_condition(field, value)
    return equals_condition(field, value) unless value.length >= 2 && value.start_with?("/") && value.end_with?("/")

    "regexp_like(CAST(#{quoted_identifier(field)} AS varchar), #{sql_string("(?i)#{value[1...-1]}")})"
  end

  def like_condition(field, value)
    "lower(CAST(#{quoted_identifier(field)} AS varchar)) LIKE lower(#{sql_string("%#{escape_like(value)}%")}) ESCAPE '!'"
  end

  def target_log_types(filters)
    self.class::LOG_TYPES.include?(filters["log_type"]) ? [filters["log_type"]] : self.class::LOG_TYPES
  end

  def table_for_log_type(log_type)
    table_expr(log_type == "syslog" ? self.class::TRINO_SYSLOG_TABLE : self.class::TRINO_AUTHLOG_TABLE)
  end

  def table_expr(name)
    parts = name.to_s.split(".").reject(&:empty?)
    parts = [self.class::TRINO_CATALOG, self.class::TRINO_SCHEMA, name] if parts.length == 1
    parts.reject(&:empty?).map { |part| quoted_identifier(part) }.join(".")
  end

  def timestamp_expression_sql
    expression = self.class::TRINO_TIMESTAMP_EXPRESSION
    expression.empty? ? quoted_identifier(self.class::TRINO_TIMESTAMP_COLUMN) : expression
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
    rows, columns = trino_client.execute(build_query(filters), timeout: 60)
    rows_to_logs(rows, columns, filters)
  end

  def rows_to_logs(rows, columns, filters)
    rows.each_with_index.map do |row, index|
      source = columns.zip(row).to_h
      source.delete("total_count")
      source.merge(
        "id" => ((filters["page"] - 1) * filters["size"]) + index,
        "index" => "#{self.class::TRINO_CATALOG}.#{self.class::TRINO_SCHEMA}",
        "display_time" => format_timestamp(source["event_time"])
      )
    end
  end

  def search_logs_page(trino_client, filters)
    rows, columns = trino_client.execute(build_query(filters), timeout: 60)
    total_index = columns.index("total_count")
    total = total_index ? rows.dig(0, total_index).to_i : 0
    logs = rows_to_logs(rows, columns, filters)
    {
      count: logs.length,
      total: total,
      page: filters["page"],
      size: filters["size"],
      total_pages: [(total.to_f / filters["size"]).ceil, 1].max,
      logs: logs
    }
  end

  def positive_int(value, fallback)
    parsed = Integer(value)
    parsed.positive? ? parsed : fallback
  rescue ArgumentError, TypeError
    fallback
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
end
