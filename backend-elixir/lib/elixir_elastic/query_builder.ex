defmodule ElixirElastic.QueryBuilder do
  @moduledoc false

  @log_types ["syslog", "authlog"]
  @jst_offset_seconds 9 * 60 * 60

  def build_query(filters) do
    size = filters["size"]
    offset = (filters["page"] - 1) * size
    select_list = if filters["skip_total"], do: "logs.*", else: "logs.*, count(*) OVER() AS total_count"
    "SELECT #{select_list} FROM (\n#{union_query(filters)}\n) logs\nORDER BY event_time DESC\nOFFSET #{offset}\nLIMIT #{size}"
  end

  def build_count_query(filters) do
    "SELECT count(*) AS total FROM (\n#{union_query(filters)}\n) logs"
  end

  def build_summary_query(date) do
    day = sql_string(Date.to_iso8601(date))

    "SELECT COALESCE(sum(total), 0) AS total FROM (\n" <>
      "SELECT COALESCE(sum(cnt), 0) AS total FROM #{table_expr(syslog_host_1m_table())} WHERE dt = DATE #{day}\n" <>
      "UNION ALL\n" <>
      "SELECT COALESCE(sum(cnt), 0) AS total FROM #{table_expr(authlog_host_1m_table())} WHERE dt = DATE #{day}\n" <>
      ") summaries"
  end

  def today_jst do
    DateTime.utc_now()
    |> DateTime.add(@jst_offset_seconds, :second)
    |> DateTime.to_date()
  end

  def time_bound("", :from, date), do: "#{Date.to_iso8601(date)} 00:00:00"
  def time_bound(nil, :from, date), do: "#{Date.to_iso8601(date)} 00:00:00"
  def time_bound("", :to, date), do: "#{Date.to_iso8601(date)} 23:59:59"
  def time_bound(nil, :to, date), do: "#{Date.to_iso8601(date)} 23:59:59"

  def time_bound(value, direction, date) do
    value = String.trim(to_string(value))

    cond do
      String.contains?(value, "T") ->
        case parse_bound_datetime(value) do
          {:ok, datetime} ->
            datetime
            |> DateTime.add(@jst_offset_seconds, :second)
            |> Calendar.strftime("%Y-%m-%d %H:%M:%S")

          :error ->
            fallback_time_bound(direction, date)
        end

      true ->
        case Time.from_iso8601(add_seconds(value)) do
          {:ok, time} -> "#{Date.to_iso8601(date)} #{Calendar.strftime(time, "%H:%M:%S")}"
          {:error, _reason} -> fallback_time_bound(direction, date)
        end
    end
  end

  defp union_query(filters) do
    filters
    |> target_log_types()
    |> Enum.map(&select_for_log_type(&1, filters))
    |> Enum.join("\nUNION ALL\n")
  end

  defp select_for_log_type(log_type, filters) do
    date = target_date(filters)
    timestamp_sql = timestamp_expression_sql()
    from = time_bound(filters["time_from"], :from, date)
    to = time_bound(filters["time_to"], :to, date)

    conditions =
      [
        "#{timestamp_sql} >= TIMESTAMP #{sql_string(from)}",
        "#{timestamp_sql} <= TIMESTAMP #{sql_string(to)}"
      ]
      |> append_match(filters["host"], "host")
      |> append_match(filters["program"], "program")
      |> append_like(filters["message"], "msg")

    """
    SELECT
      #{timestamp_sql} AS event_time,
      CAST(#{quoted_identifier("host")} AS varchar) AS host,
      CAST(#{quoted_identifier("program")} AS varchar) AS program,
      CAST(#{quoted_identifier("msg")} AS varchar) AS msg,
      #{sql_string(log_type)} AS log_type
    FROM #{table_for_log_type(log_type)}
    WHERE #{Enum.join(conditions, " AND ")}
    """
  end

  defp target_date(%{"date" => value}) when is_binary(value) do
    case Date.from_iso8601(value) do
      {:ok, date} -> date
      _ -> today_jst()
    end
  end

  defp target_date(_filters), do: today_jst()

  defp append_like(conditions, value, field) when value not in [nil, ""] do
    conditions ++
      [
        "lower(CAST(#{quoted_identifier(field)} AS varchar)) LIKE lower(#{sql_string("%#{escape_like(value)}%")}) ESCAPE '!'"
      ]
  end

  defp append_like(conditions, _value, _field), do: conditions

  defp append_match(conditions, value, field) when value not in [nil, ""] do
    if String.length(value) >= 2 && String.starts_with?(value, "/") && String.ends_with?(value, "/") do
      pattern = String.slice(value, 1, String.length(value) - 2)
      conditions ++ ["regexp_like(CAST(#{quoted_identifier(field)} AS varchar), #{sql_string("(?i)#{pattern}")})"]
    else
      conditions ++ ["lower(CAST(#{quoted_identifier(field)} AS varchar)) = lower(#{sql_string(value)})"]
    end
  end

  defp append_match(conditions, _value, _field), do: conditions

  defp target_log_types(%{"log_type" => log_type}) when log_type in @log_types, do: [log_type]
  defp target_log_types(_filters), do: @log_types

  defp table_for_log_type("syslog"), do: table_expr(syslog_table())
  defp table_for_log_type("authlog"), do: table_expr(authlog_table())

  defp table_expr(name) do
    parts = String.split(name, ".", trim: true)
    parts = if length(parts) == 1, do: [trino_catalog(), trino_schema(), name], else: parts

    parts
    |> Enum.reject(&(&1 in [nil, ""]))
    |> Enum.map_join(".", &quoted_identifier/1)
  end

  defp timestamp_expression_sql do
    case timestamp_expression() do
      "" -> quoted_identifier(timestamp_column())
      expression -> expression
    end
  end

  defp quoted_identifier(value), do: ~s("#{String.replace(to_string(value), "\"", "\"\"")}")
  defp sql_string(value), do: "'#{String.replace(to_string(value), "'", "''")}'"

  defp escape_like(value) do
    value
    |> to_string()
    |> String.replace("!", "!!")
    |> String.replace("%", "!%")
    |> String.replace("_", "!_")
  end

  defp parse_bound_datetime(value) do
    cond do
      match?({:ok, _, _}, DateTime.from_iso8601(value)) ->
        {:ok, datetime, _offset} = DateTime.from_iso8601(value)
        {:ok, datetime}

      match?({:ok, _}, NaiveDateTime.from_iso8601(value)) ->
        {:ok, naive} = NaiveDateTime.from_iso8601(value)
        {:ok, DateTime.from_naive!(naive, "Etc/UTC") |> DateTime.add(-@jst_offset_seconds, :second)}

      true ->
        :error
    end
  end

  defp fallback_time_bound(:to, date), do: "#{Date.to_iso8601(date)} 23:59:59"
  defp fallback_time_bound(_direction, date), do: "#{Date.to_iso8601(date)} 00:00:00"
  defp add_seconds(value), do: if(String.length(value) == 5, do: value <> ":00", else: value)

  defp trino_catalog, do: Application.fetch_env!(:elixir_elastic, :trino_catalog)
  defp trino_schema, do: Application.fetch_env!(:elixir_elastic, :trino_schema)
  defp syslog_table, do: Application.fetch_env!(:elixir_elastic, :syslog_table)
  defp authlog_table, do: Application.fetch_env!(:elixir_elastic, :authlog_table)
  defp syslog_host_1m_table, do: Application.fetch_env!(:elixir_elastic, :syslog_host_1m_table)
  defp authlog_host_1m_table, do: Application.fetch_env!(:elixir_elastic, :authlog_host_1m_table)
  defp timestamp_column, do: Application.fetch_env!(:elixir_elastic, :timestamp_column)
  defp timestamp_expression, do: Application.fetch_env!(:elixir_elastic, :timestamp_expression)
end
